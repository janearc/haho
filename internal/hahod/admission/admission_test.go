package admission

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/janearc/haho/gen/filterpb"
	"github.com/janearc/haho/gen/jobpb"
)

func servableSpec() *jobpb.JobSpec {
	return &jobpb.JobSpec{
		JobId: "test-1",
		Payload: &jobpb.Payload{
			Body: &jobpb.Payload_Text{Text: "hello"},
		},
		Backend: &jobpb.Backend{
			Kind:    jobpb.Backend_ANTHROPIC_COMPATIBLE,
			BaseUrl: "http://127.0.0.1:1",
			Model:   "claude-fable-5",
		},
		Framing:      &jobpb.Framing{Strategy: jobpb.Framing_WHOLE_OR_ERROR},
		Synthesis:    &jobpb.Synthesis{Mode: jobpb.Synthesis_NONE},
		Cache:        &jobpb.Cache{Kind: jobpb.Cache_NONE},
		AllowPartial: false,
	}
}

func TestLaneOf(t *testing.T) {
	cases := []struct {
		kind jobpb.Backend_Kind
		lane jobpb.Lane
	}{
		{jobpb.Backend_ANTHROPIC_COMPATIBLE, jobpb.Lane_LANE_API},
		{jobpb.Backend_OPENAI_COMPATIBLE, jobpb.Lane_LANE_API},
		{jobpb.Backend_MLX, jobpb.Lane_LANE_METAL},
		{jobpb.Backend_AFM, jobpb.Lane_LANE_ANE},
		{jobpb.Backend_SPEECH, jobpb.Lane_LANE_ANE},
		{jobpb.Backend_TRANSLATION, jobpb.Lane_LANE_ANE},
		{jobpb.Backend_KIND_UNSPECIFIED, jobpb.Lane_LANE_UNSPECIFIED},
	}
	for _, c := range cases {
		if got := LaneOf(c.kind); got != c.lane {
			t.Errorf("LaneOf(%s) = %s, want %s", c.kind, got, c.lane)
		}
	}
}

func TestAdmitServableSpec(t *testing.T) {
	a := New(Defaults())
	ticket, ref := a.Admit(servableSpec())
	if ref != nil {
		t.Fatalf("servable spec refused: %v", ref)
	}
	if ticket.Queued() {
		t.Fatal("empty lane should admit straight to a slot, not queue")
	}
	if ticket.Lane != jobpb.Lane_LANE_API {
		t.Fatalf("ticket lane = %s, want LANE_API", ticket.Lane)
	}
	ticket.Release()
}

func TestRefusals(t *testing.T) {
	cases := []struct {
		name   string
		code   string
		mutate func(*jobpb.JobSpec)
	}{
		{"empty job id", "empty_job_id", func(s *jobpb.JobSpec) { s.JobId = "" }},
		{"unknown kind", "unknown_backend_kind", func(s *jobpb.JobSpec) { s.Backend.Kind = jobpb.Backend_KIND_UNSPECIFIED }},
		{"metal lane unserved", "lane_not_served", func(s *jobpb.JobSpec) { s.Backend.Kind = jobpb.Backend_MLX }},
		{"ane lane unserved", "lane_not_served", func(s *jobpb.JobSpec) { s.Backend.Kind = jobpb.Backend_AFM }},
		{"resize unserved", "framing_not_served", func(s *jobpb.JobSpec) { s.Framing.Strategy = jobpb.Framing_RESIZE_ON_FAIL }},
		{"framing unset", "framing_not_served", func(s *jobpb.JobSpec) { s.Framing = nil }},
		{"mapesis unserved", "synthesis_not_served", func(s *jobpb.JobSpec) { s.Synthesis.Mode = jobpb.Synthesis_MAPESIS }},
		{"cache unserved", "cache_not_served", func(s *jobpb.JobSpec) { s.Cache.Kind = jobpb.Cache_SURREALDB }},
		{"filters unserved", "filters_not_served", func(s *jobpb.JobSpec) {
			s.InputFilters = &filterpb.FilterPipelineConfig{
				Filters: []*filterpb.FilterConfig{{Name: "rtk", Type: filterpb.FilterType_FILTER_TYPE_RTK}},
			}
		}},
		{"raw payload", "payload_not_servable", func(s *jobpb.JobSpec) {
			s.Payload = &jobpb.Payload{Body: &jobpb.Payload_Raw{Raw: []byte{1}}}
		}},
		{"payload absent", "payload_not_servable", func(s *jobpb.JobSpec) { s.Payload = nil }},
		{"secret env unset", "secret_env_missing", func(s *jobpb.JobSpec) {
			s.SecretEnv = map[string]string{"api_key": "HAHO_TEST_UNSET_VAR"}
		}},
		{"token env unset", "secret_env_missing", func(s *jobpb.JobSpec) {
			s.Backend.TokenEnv = "HAHO_TEST_UNSET_VAR"
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := New(Defaults())
			spec := servableSpec()
			c.mutate(spec)
			ticket, ref := a.Admit(spec)
			if ref == nil {
				ticket.Release()
				t.Fatal("expected refusal, got admission")
			}
			if ref.Code != c.code {
				t.Fatalf("refusal code = %s (%s), want %s", ref.Code, ref.Reason, c.code)
			}
		})
	}
}

// TestOpenAIKindAdmitsWithoutCredential pins the local door: the
// /api/chat-shaped kind is served and needs no token_env — local ollama
// is credential-free by design.
func TestOpenAIKindAdmitsWithoutCredential(t *testing.T) {
	a := New(Defaults())
	spec := servableSpec()
	spec.Backend.Kind = jobpb.Backend_OPENAI_COMPATIBLE
	spec.Backend.TokenEnv = ""
	ticket, ref := a.Admit(spec)
	if ref != nil {
		t.Fatalf("openai kind refused: %v", ref)
	}
	if ticket.Lane != jobpb.Lane_LANE_API {
		t.Fatalf("lane = %s, want LANE_API", ticket.Lane)
	}
	ticket.Release()
}

func TestSecretEnvPresentAdmits(t *testing.T) {
	t.Setenv("HAHO_TEST_SET_VAR", "value")
	a := New(Defaults())
	spec := servableSpec()
	spec.SecretEnv = map[string]string{"api_key": "HAHO_TEST_SET_VAR"}
	spec.Backend.TokenEnv = "HAHO_TEST_SET_VAR"
	ticket, ref := a.Admit(spec)
	if ref != nil {
		t.Fatalf("spec with set env vars refused: %v", ref)
	}
	ticket.Release()
}

func TestCapacityQueuesThenRefuses(t *testing.T) {
	b := Defaults()
	b.API = LaneCfg{Budget: 1, Queue: 1}
	a := New(b)

	first, ref := a.Admit(servableSpec())
	if ref != nil {
		t.Fatalf("first admit refused: %v", ref)
	}
	if first.Queued() {
		t.Fatal("first admit should hold the slot")
	}

	second, ref := a.Admit(servableSpec())
	if ref != nil {
		t.Fatalf("second admit refused: %v", ref)
	}
	if !second.Queued() {
		t.Fatal("second admit should queue behind the budget of 1")
	}

	_, ref = a.Admit(servableSpec())
	if ref == nil {
		t.Fatal("third admit should be REFUSED: budget 1 held, queue 1 full")
	}
	if ref.Code != "queue_full" {
		t.Fatalf("refusal code = %s, want queue_full", ref.Code)
	}

	// Releasing the slot lets the queued ticket through.
	done := make(chan struct{})
	go func() {
		if err := second.Wait(context.Background()); err != nil {
			t.Errorf("wait: %v", err)
		}
		close(done)
	}()
	first.Release()
	<-done
	second.Release()
}

// TestReleaseOnQueuedTicketSurrendersQueueNotSlot pins the blocker found in
// review: releasing a still-queued ticket must decrement the queue and must
// NOT consume a slot token another job holds (which admitted budget+1,
// leaked queue capacity, and could hang the releasing goroutine forever).
func TestReleaseOnQueuedTicketSurrendersQueueNotSlot(t *testing.T) {
	b := Defaults()
	b.API = LaneCfg{Budget: 1, Queue: 1}
	a := New(b)

	first, ref := a.Admit(servableSpec())
	if ref != nil {
		t.Fatalf("first admit refused: %v", ref)
	}
	second, ref := a.Admit(servableSpec())
	if ref != nil {
		t.Fatalf("second admit refused: %v", ref)
	}

	// Pre-fix this blocked forever or stole first's slot; now it must
	// return promptly and only surrender the queue position.
	released := make(chan struct{})
	go func() { second.Release(); close(released) }()
	select {
	case <-released:
	case <-time.After(2 * time.Second):
		t.Fatal("Release on a queued ticket blocked")
	}

	// Queue capacity is restored: a new job queues instead of REFUSED.
	third, ref := a.Admit(servableSpec())
	if ref != nil {
		t.Fatalf("queue capacity leaked: %v", ref)
	}
	if !third.Queued() {
		t.Fatal("third should queue: first still holds the only slot")
	}

	// And first still holds its slot: budget stays 1, so third is queued
	// until first releases.
	first.Release()
	if err := third.Wait(context.Background()); err != nil {
		t.Fatalf("wait after release: %v", err)
	}
	third.Release()
}

func TestReleaseIsIdempotent(t *testing.T) {
	b := Defaults()
	b.API = LaneCfg{Budget: 1, Queue: 1}
	a := New(b)
	ticket, ref := a.Admit(servableSpec())
	if ref != nil {
		t.Fatalf("admit refused: %v", ref)
	}
	ticket.Release()
	ticket.Release() // second settle is a no-op, not a slot underflow
	next, ref := a.Admit(servableSpec())
	if ref != nil {
		t.Fatalf("admit after double release refused: %v", ref)
	}
	if next.Queued() {
		t.Fatal("slot should be free after release")
	}
	next.Release()
}

// TestWaitAbortsOnContext pins shutdown semantics: a queued job abandoned
// by a dying daemon surrenders its queue position and reports the abort.
func TestWaitAbortsOnContext(t *testing.T) {
	b := Defaults()
	b.API = LaneCfg{Budget: 1, Queue: 1}
	a := New(b)
	first, _ := a.Admit(servableSpec())
	second, _ := a.Admit(servableSpec())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := second.Wait(ctx); err == nil {
		t.Fatal("Wait on a dead context must error")
	}
	second.Release() // settled; must be a safe no-op

	// The surrendered queue position is available again.
	third, ref := a.Admit(servableSpec())
	if ref != nil {
		t.Fatalf("queue position leaked on abort: %v", ref)
	}
	third.Release()
	first.Release()
}

// TestDefaultsPinTheSection10Table keeps the RFC numbers honest: a typo'd
// budget passes every behavioral test, so the table itself is pinned.
func TestDefaultsPinTheSection10Table(t *testing.T) {
	b := Defaults()
	if b.Metal != (LaneCfg{Budget: 1, Queue: 4}) {
		t.Fatalf("metal = %+v", b.Metal)
	}
	if b.API != (LaneCfg{Budget: 16, Queue: 64}) {
		t.Fatalf("api = %+v", b.API)
	}
	if b.CPU != (LaneCfg{Budget: 8, Queue: 32}) {
		t.Fatalf("cpu = %+v", b.CPU)
	}
	if b.ANE.Queue != 8 || b.ANEDoors["afm"] != 1 || b.ANEDoors["speech"] != 3 {
		t.Fatalf("ane = %+v doors %+v", b.ANE, b.ANEDoors)
	}

	// And the api lane genuinely admits 16 concurrent, queues 64, refuses
	// the 81st.
	a := New(b)
	var tickets []*Ticket
	for i := range 16 {
		tk, ref := a.Admit(servableSpec())
		if ref != nil {
			t.Fatalf("admit %d refused: %v", i, ref)
		}
		if tk.Queued() {
			t.Fatalf("admit %d queued below budget", i)
		}
		tickets = append(tickets, tk)
	}
	for i := range 64 {
		tk, ref := a.Admit(servableSpec())
		if ref != nil {
			t.Fatalf("queue admit %d refused: %v", i, ref)
		}
		if !tk.Queued() {
			t.Fatalf("queue admit %d got a slot past budget", i)
		}
		tickets = append(tickets, tk)
	}
	if _, ref := a.Admit(servableSpec()); ref == nil || ref.Code != "queue_full" {
		t.Fatalf("81st admit = %v, want queue_full", ref)
	}
	for _, tk := range tickets {
		tk.Release()
	}
}

// cliSpecFor is servableSpec turned into a CLAUDE_CLI job: same servable
// shape, and the kind carries no base_url because it refuses one.
func cliSpecFor(id string) *jobpb.JobSpec {
	s := servableSpec()
	s.JobId = id
	s.Backend = &jobpb.Backend{Kind: jobpb.Backend_CLAUDE_CLI, Model: "claude-fable-5"}
	return s
}

// apiSpecFor is servableSpec with an id of its own.
func apiSpecFor(id string) *jobpb.JobSpec {
	s := servableSpec()
	s.JobId = id
	return s
}

func TestClaudeCLIAdmitsOneAtATime(t *testing.T) {
	// The operator ruled 2026-08-21 that two judge runs must never overlap:
	// a CLAUDE_CLI job is an agent session spending her own allotment, so two
	// at once is expense rather than slowness. The ledger already carries a
	// duplicate pair (delightd PR 131, rulings 030637Z and 031731Z).
	a := New(Defaults())

	first, ref := a.Admit(cliSpecFor("ruling-1"))
	if ref != nil {
		t.Fatalf("first CLAUDE_CLI job refused: %v", ref)
	}
	if first.Queued() {
		t.Fatal("first CLAUDE_CLI job queued; it should hold the only slot")
	}

	second, ref := a.Admit(cliSpecFor("ruling-2"))
	if ref != nil {
		t.Fatalf("second CLAUDE_CLI job refused outright; it should QUEUE: %v", ref)
	}
	if !second.Queued() {
		t.Fatal("second CLAUDE_CLI job was admitted to run concurrently with the first")
	}

	// Releasing the first must hand the slot to the waiter, not strand it.
	first.Release()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := second.Wait(ctx); err != nil {
		t.Fatalf("queued CLAUDE_CLI job never got the freed slot: %v", err)
	}
	second.Release()
}

func TestClaudeCLIDoesNotConsumeApiLaneSlots(t *testing.T) {
	// The stated departure: a CLI job takes the cli gate INSTEAD of the api
	// gate. This pins that behaviour so it is a decision on the record rather
	// than something a later reader has to infer.
	a := New(Defaults())
	held, ref := a.Admit(cliSpecFor("ruling-1"))
	if ref != nil {
		t.Fatalf("CLAUDE_CLI refused: %v", ref)
	}
	defer held.Release()

	// The api lane should still take its full budget with the CLI job running.
	var tickets []*Ticket
	for i := 0; i < Defaults().API.Budget; i++ {
		tk, ref := a.Admit(apiSpecFor(fmt.Sprintf("api-%d", i)))
		if ref != nil {
			t.Fatalf("api job %d refused while a CLI job held its own gate: %v", i, ref)
		}
		if tk.Queued() {
			t.Fatalf("api job %d queued; the CLI job should not have taken an api slot", i)
		}
		tickets = append(tickets, tk)
	}
	for _, tk := range tickets {
		tk.Release()
	}
}

func TestClaudeCLIQueueIsBounded(t *testing.T) {
	// A full queue is REFUSED, never silently held past the bound, and the
	// refusal names CLAUDE_CLI rather than the api lane so the operator can
	// tell which bound she hit.
	a := New(Defaults())
	held, _ := a.Admit(cliSpecFor("run"))
	defer held.Release()

	for i := 0; i < Defaults().CLI.Queue; i++ {
		if _, ref := a.Admit(cliSpecFor(fmt.Sprintf("q-%d", i))); ref != nil {
			t.Fatalf("queued job %d refused before the bound: %v", i, ref)
		}
	}
	_, ref := a.Admit(cliSpecFor("one-too-many"))
	if ref == nil {
		t.Fatal("a job past the queue bound was admitted")
	}
	if ref.Code != "queue_full" {
		t.Errorf("refusal code = %q, want queue_full", ref.Code)
	}
	if !strings.Contains(ref.Reason, "CLAUDE_CLI") {
		t.Errorf("refusal %q does not name CLAUDE_CLI; the operator cannot tell which bound she hit", ref.Reason)
	}
}
