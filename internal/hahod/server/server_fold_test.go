package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/janearc/haho/gen/jobpb"
	"github.com/janearc/haho/internal/hahod/admission"
	"github.com/janearc/haho/internal/hahod/store"
)

// This file holds the tests added by the adversarial review fold: each
// pins a behavior that was demonstrated broken (or unpinned) before the
// corresponding fix landed.

func newTestServerWith(t *testing.T, ctx context.Context, budgets admission.Budgets) (*httptest.Server, *Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "hahod.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	srv := New(ctx, st, admission.New(budgets),
		slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelError})),
		"http://hahod.test")
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, srv, st
}

// blockingModel serves the messages API but holds every request until
// released.
func blockingModel(t *testing.T) (*httptest.Server, chan struct{}) {
	t.Helper()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.Write([]byte(`{"model":"m","content":[{"type":"text","text":"done"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, release
}

func mustSession(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	status, created := postJSON(t, ts.URL+"/sessions", nil)
	if status != http.StatusCreated {
		t.Fatalf("new session: %d %v", status, created)
	}
	return created["id"].(string)
}

func pollDisposition(t *testing.T, ts *httptest.Server, id string, want string, deadline time.Duration) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		_, st := getJSON(t, fmt.Sprintf("%s/sessions/%s", ts.URL, id))
		if st["disposition"] == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("session %s never reached disposition %s", id, want)
}

// TestQueuedJobRunsWhenSlotFrees pins the scheduler end to end: budget 1,
// job A holds the lane, job B queues, A finishes, B runs and lands.
func TestQueuedJobRunsWhenSlotFrees(t *testing.T) {
	b := admission.Defaults()
	b.API = admission.LaneCfg{Budget: 1, Queue: 1}
	ts, _, _ := newTestServerWith(t, context.Background(), b)
	model, release := blockingModel(t)

	a := mustSession(t, ts)
	bID := mustSession(t, ts)
	if status, resp := postJSON(t, fmt.Sprintf("%s/sessions/%s/task", ts.URL, a), specJSON(t, model.URL)); status != http.StatusAccepted {
		t.Fatalf("task A: %d %v", status, resp)
	}
	if status, resp := postJSON(t, fmt.Sprintf("%s/sessions/%s/task", ts.URL, bID), specJSON(t, model.URL)); status != http.StatusAccepted {
		t.Fatalf("task B (queued): %d %v", status, resp)
	}

	// B is admitted but in flight behind A, with no disposition yet.
	_, st := getJSON(t, fmt.Sprintf("%s/sessions/%s", ts.URL, bID))
	if st["in_flight"] != true || st["disposition"] != "" {
		t.Fatalf("queued B should be in flight with no disposition, got %v", st)
	}

	close(release) // A completes, the slot frees, B runs
	pollDisposition(t, ts, a, "PRODUCED", 10*time.Second)
	pollDisposition(t, ts, bID, "PRODUCED", 10*time.Second)
}

// TestQueueFullIs429 pins RFC section 10 through the HTTP surface: a job
// arriving at a full queue is REFUSED, loudly, before spending — and the
// refused session stays quiescent and taskable.
func TestQueueFullIs429(t *testing.T) {
	b := admission.Defaults()
	b.API = admission.LaneCfg{Budget: 1, Queue: 1}
	ts, _, _ := newTestServerWith(t, context.Background(), b)
	model, release := blockingModel(t)
	defer close(release)

	first := mustSession(t, ts)
	second := mustSession(t, ts)
	third := mustSession(t, ts)
	postJSON(t, fmt.Sprintf("%s/sessions/%s/task", ts.URL, first), specJSON(t, model.URL))
	postJSON(t, fmt.Sprintf("%s/sessions/%s/task", ts.URL, second), specJSON(t, model.URL))

	status, resp := postJSON(t, fmt.Sprintf("%s/sessions/%s/task", ts.URL, third), specJSON(t, model.URL))
	if status != http.StatusTooManyRequests {
		t.Fatalf("status = %d %v, want 429", status, resp)
	}
	if resp["code"] != "queue_full" {
		t.Fatalf("code = %v, want queue_full", resp["code"])
	}
	_, st := getJSON(t, fmt.Sprintf("%s/sessions/%s", ts.URL, third))
	if st["in_flight"] != false || st["disposition"] != "" {
		t.Fatalf("refused session must stay quiescent, got %v", st)
	}
}

// TestDoubleTaskRaceExactlyOneClaims pins the TOCTOU fix: two concurrent
// tasks on one session yield exactly one 202 and one 409, never a 500 and
// never two admissions. Pre-fix this answered 500 in 300 of 300 rounds.
func TestDoubleTaskRaceExactlyOneClaims(t *testing.T) {
	model, release := blockingModel(t)
	close(release)

	for range 50 {
		ts, _, _ := func() (*httptest.Server, *Server, *store.Store) {
			return newTestServerWith(t, context.Background(), admission.Defaults())
		}()
		id := mustSession(t, ts)
		spec := specJSON(t, model.URL)

		var mu sync.Mutex
		statuses := map[int]int{}
		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				resp, err := http.Post(fmt.Sprintf("%s/sessions/%s/task", ts.URL, id), "application/json", bytes.NewReader(spec))
				if err != nil {
					t.Errorf("post: %v", err)
					return
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				mu.Lock()
				statuses[resp.StatusCode]++
				mu.Unlock()
			}()
		}
		wg.Wait()
		if statuses[http.StatusAccepted] != 1 || statuses[http.StatusConflict] != 1 {
			t.Fatalf("statuses = %v, want exactly one 202 and one 409", statuses)
		}
		pollDisposition(t, ts, id, "PRODUCED", 10*time.Second)
		ts.Close()
	}
}

// TestFailedLandingCarriesReason pins the reason relay end to end: the
// endpoint's stated failure rides the completion through store and HTTP.
func TestFailedLandingCarriesReason(t *testing.T) {
	ts, _, _ := newTestServerWith(t, context.Background(), admission.Defaults())
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"overloaded"}}`))
	}))
	defer model.Close()

	id := mustSession(t, ts)
	postJSON(t, fmt.Sprintf("%s/sessions/%s/task", ts.URL, id), specJSON(t, model.URL))
	pollDisposition(t, ts, id, "FAILED", 10*time.Second)

	resp, err := http.Get(fmt.Sprintf("%s/sessions/%s/completion", ts.URL, id))
	if err != nil {
		t.Fatalf("GET completion: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	completion := &jobpb.JobCompletion{}
	if err := protojson.Unmarshal(raw, completion); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if completion.GetOutcome() != jobpb.JobCompletion_FAILED {
		t.Fatalf("outcome = %s", completion.GetOutcome())
	}
	if !bytes.Contains([]byte(completion.GetReason()), []byte("overloaded")) {
		t.Fatalf("reason = %q; the endpoint's reason must ride the completion", completion.GetReason())
	}
	if completion.GetResult() != nil {
		t.Fatal("a FAILED completion never fabricates output")
	}
}

// TestSecretEnvRefusedOverHTTP pins the contract's admission validation at
// the wire: a spec naming an unset env var is 422 secret_env_missing and
// spends nothing; with the var set, the same spec admits.
func TestSecretEnvRefusedOverHTTP(t *testing.T) {
	ts, _, _ := newTestServerWith(t, context.Background(), admission.Defaults())
	model := stubModel(t)

	spec := &jobpb.JobSpec{
		JobId:   "sec-1",
		Payload: &jobpb.Payload{Body: &jobpb.Payload_Text{Text: "x"}},
		Backend: &jobpb.Backend{
			Kind: jobpb.Backend_ANTHROPIC_COMPATIBLE, BaseUrl: model.URL, Model: "m",
		},
		Framing:   &jobpb.Framing{Strategy: jobpb.Framing_WHOLE_OR_ERROR},
		SecretEnv: map[string]string{"api_key": "HAHO_TEST_HTTP_SECRET"},
	}
	raw, _ := protojson.Marshal(spec)

	id := mustSession(t, ts)
	status, resp := postJSON(t, fmt.Sprintf("%s/sessions/%s/task", ts.URL, id), raw)
	if status != http.StatusUnprocessableEntity || resp["code"] != "secret_env_missing" {
		t.Fatalf("unset var: %d %v, want 422 secret_env_missing", status, resp)
	}

	t.Setenv("HAHO_TEST_HTTP_SECRET", "value")
	status, resp = postJSON(t, fmt.Sprintf("%s/sessions/%s/task", ts.URL, id), raw)
	if status != http.StatusAccepted {
		t.Fatalf("set var: %d %v, want 202 (the refusal spent nothing)", status, resp)
	}
	pollDisposition(t, ts, id, "PRODUCED", 10*time.Second)
}

// TestCompletionReadBackIsStoredBytes pins store.go's byte-identity claim:
// the HTTP body is the stored protojson, byte for byte, twice.
func TestCompletionReadBackIsStoredBytes(t *testing.T) {
	ts, _, st := newTestServerWith(t, context.Background(), admission.Defaults())
	model := stubModel(t)
	id := mustSession(t, ts)
	postJSON(t, fmt.Sprintf("%s/sessions/%s/task", ts.URL, id), specJSON(t, model.URL))
	pollDisposition(t, ts, id, "PRODUCED", 10*time.Second)

	read := func() []byte {
		resp, err := http.Get(fmt.Sprintf("%s/sessions/%s/completion", ts.URL, id))
		if err != nil {
			t.Fatalf("GET completion: %v", err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return raw
	}
	first := read()
	sess, err := st.Get(id)
	if err != nil {
		t.Fatalf("store get: %v", err)
	}
	if !bytes.Equal(first, sess.Completion) {
		t.Fatalf("HTTP body differs from stored bytes:\n%s\nvs\n%s", first, sess.Completion)
	}
	if !bytes.Equal(first, read()) {
		t.Fatal("second read differs from first")
	}
}

// TestMintRetriesOnCollision pins the re-mint loop: colliding ids are
// retried loudly instead of surfacing a spurious 500.
func TestMintRetriesOnCollision(t *testing.T) {
	ts, srv, _ := newTestServerWith(t, context.Background(), admission.Defaults())
	first := mustSession(t, ts)

	ids := []string{first, first, "haho-1-fresh"} // two collisions, then unique
	srv.idFn = func() (string, error) {
		id := ids[0]
		if len(ids) > 1 {
			ids = ids[1:]
		}
		return id, nil
	}
	status, created := postJSON(t, ts.URL+"/sessions", nil)
	if status != http.StatusCreated || created["id"] != "haho-1-fresh" {
		t.Fatalf("mint with collisions: %d %v", status, created)
	}
}

// TestSessionIDShape pins the RFC's console example shape.
func TestSessionIDShape(t *testing.T) {
	ts, _, _ := newTestServerWith(t, context.Background(), admission.Defaults())
	id := mustSession(t, ts)
	if !regexp.MustCompile(`^haho-\d+-[0-9a-f]{5}$`).MatchString(id) {
		t.Fatalf("id %q does not match the RFC shape haho-<pid>-<5 hex>", id)
	}
}

// TestDrainLandsEverythingOnShutdown pins the drain-and-land claim that
// review demonstrated false: with the base context cancelled, a running
// job lands FAILED with the cancellation as its reason and a queued job
// lands FAILED without running — nothing is left in flight after Drain.
func TestDrainLandsEverythingOnShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	b := admission.Defaults()
	b.API = admission.LaneCfg{Budget: 1, Queue: 1}
	ts, srv, st := newTestServerWith(t, ctx, b)
	model, release := blockingModel(t)
	defer close(release)

	running := mustSession(t, ts)
	queued := mustSession(t, ts)
	postJSON(t, fmt.Sprintf("%s/sessions/%s/task", ts.URL, running), specJSON(t, model.URL))
	postJSON(t, fmt.Sprintf("%s/sessions/%s/task", ts.URL, queued), specJSON(t, model.URL))

	cancel() // the daemon is shutting down
	done := make(chan struct{})
	go func() { srv.Drain(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Drain did not complete; execute goroutines leaked")
	}

	for _, id := range []string{running, queued} {
		sess, err := st.Get(id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if sess.State != store.StateLanded {
			t.Fatalf("session %s is %s after Drain; nothing may exit half-recorded", id, sess.State)
		}
		completion := &jobpb.JobCompletion{}
		if err := protojson.Unmarshal(sess.Completion, completion); err != nil {
			t.Fatalf("completion for %s: %v", id, err)
		}
		if completion.GetOutcome() != jobpb.JobCompletion_FAILED {
			t.Fatalf("outcome for %s = %s, want FAILED with the shutdown as reason", id, completion.GetOutcome())
		}
	}
}

// TestRecoverOrphans pins the startup sweep: queued/running rows land
// FAILED loudly; created and landed rows are untouched.
func TestRecoverOrphans(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "hahod.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	if _, err := st.Create("haho-1-quiet"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Create("haho-1-queue"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition("haho-1-queue", store.StateQueued, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Create("haho-1-runng"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition("haho-1-runng", store.StateRunning, func(s *store.Session) { s.JobID = "job-r" }); err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelError}))
	n, err := RecoverOrphans(st, log)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if n != 2 {
		t.Fatalf("recovered %d, want 2", n)
	}
	for _, id := range []string{"haho-1-queue", "haho-1-runng"} {
		sess, _ := st.Get(id)
		if sess.State != store.StateLanded {
			t.Fatalf("%s is %s, want landed", id, sess.State)
		}
		completion := &jobpb.JobCompletion{}
		if err := protojson.Unmarshal(sess.Completion, completion); err != nil {
			t.Fatalf("completion: %v", err)
		}
		if completion.GetOutcome() != jobpb.JobCompletion_FAILED {
			t.Fatalf("outcome = %s, want FAILED", completion.GetOutcome())
		}
	}
	quiet, _ := st.Get("haho-1-quiet")
	if quiet.State != store.StateCreated {
		t.Fatalf("quiescent session touched by recovery: %s", quiet.State)
	}
}
