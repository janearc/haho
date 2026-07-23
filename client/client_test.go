package client

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/janearc/haho/gen/jobpb"
	"github.com/janearc/haho/internal/hahod/admission"
	"github.com/janearc/haho/internal/hahod/server"
	"github.com/janearc/haho/internal/hahod/store"
)

// newDaemon wires the real server stack onto httptest, so the client is
// tested against the actual thing it dials.
func newDaemon(t *testing.T) *httptest.Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "hahod.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	srv := server.New(context.Background(), st, admission.New(admission.Defaults()),
		slog.New(slog.NewTextHandler(discard{}, &slog.HandlerOptions{Level: slog.LevelError})),
		"http://hahod.test")
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func stubModel(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"model":"m","message":{"content":"hoi mesh"},"prompt_eval_count":3,"eval_count":2,"done":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func specFor(modelURL string) *jobpb.JobSpec {
	return &jobpb.JobSpec{
		JobId:   "client-test-1",
		Payload: &jobpb.Payload{Body: &jobpb.Payload_Text{Text: "hello"}},
		Backend: &jobpb.Backend{
			Kind:    jobpb.Backend_OPENAI_COMPATIBLE,
			BaseUrl: modelURL,
			Model:   "m",
		},
		Framing: &jobpb.Framing{Strategy: jobpb.Framing_WHOLE_OR_ERROR},
	}
}

// TestEndToEnd is the RFC walkthrough through the client: dial, new,
// task, wait, completion.
func TestEndToEnd(t *testing.T) {
	daemon := newDaemon(t)
	model := stubModel(t)
	c := Dial(daemon.URL)
	ctx := context.Background()

	session, err := c.New(ctx)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	statusAddr, err := c.Task(ctx, session, specFor(model.URL))
	if err != nil {
		t.Fatalf("task: %v", err)
	}
	if statusAddr == "" {
		t.Fatal("acceptance carries the status address")
	}
	completion, err := c.Wait(ctx, session, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if completion.GetOutcome() != jobpb.JobCompletion_PRODUCED {
		t.Fatalf("outcome = %s (%s)", completion.GetOutcome(), completion.GetReason())
	}
	if completion.GetResult().GetText() != "hoi mesh" {
		t.Fatalf("result = %q", completion.GetResult().GetText())
	}
}

// TestRefusedSurfacesAsItself pins the loud no: an admission refusal
// arrives as *Refused with the daemon's code, never as generic transport
// trouble.
func TestRefusedSurfacesAsItself(t *testing.T) {
	daemon := newDaemon(t)
	c := Dial(daemon.URL)
	ctx := context.Background()

	session, err := c.New(ctx)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	spec := specFor("http://127.0.0.1:1")
	spec.Backend.Kind = jobpb.Backend_MLX
	_, err = c.Task(ctx, session, spec)
	var refused *Refused
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v, want *Refused", err)
	}
	if refused.Code != "lane_not_served" {
		t.Fatalf("code = %s", refused.Code)
	}
}

// TestOffContractSpecNeverLeaves pins client-side strictness: a spec with
// an unknown field fails before any request is made.
func TestOffContractSpecNeverLeaves(t *testing.T) {
	var hits atomic.Int32
	sentry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer sentry.Close()
	c := Dial(sentry.URL)

	_, err := c.TaskRaw(context.Background(), "haho-0-xxxxx",
		[]byte(`{"jobId":"x","deadline":{"budgetMs":500}}`))
	if err == nil {
		t.Fatal("off-contract spec must fail client-side")
	}
	if hits.Load() != 0 {
		t.Fatal("the off-contract spec left the machine")
	}
}

// TestCompletionBeforeLandingIsAnError pins the 409 surface: pending is
// an error naming the state, distinguishable from not-found.
func TestCompletionBeforeLandingIsAnError(t *testing.T) {
	daemon := newDaemon(t)
	c := Dial(daemon.URL)
	session, err := c.New(context.Background())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := c.Completion(context.Background(), session); err == nil {
		t.Fatal("completion before landing must error")
	}
}
