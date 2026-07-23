package admission

import (
	"testing"

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
	if ticket.Queued {
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
		{"openai kind unserved", "backend_kind_not_served", func(s *jobpb.JobSpec) { s.Backend.Kind = jobpb.Backend_OPENAI_COMPATIBLE }},
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
	if first.Queued {
		t.Fatal("first admit should hold the slot")
	}

	second, ref := a.Admit(servableSpec())
	if ref != nil {
		t.Fatalf("second admit refused: %v", ref)
	}
	if !second.Queued {
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
		second.Wait()
		close(done)
	}()
	first.Release()
	<-done
	second.Release()
}
