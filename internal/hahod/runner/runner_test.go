package runner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/janearc/haho/gen/jobpb"
)

func specFor(url string) *jobpb.JobSpec {
	return &jobpb.JobSpec{
		JobId:   "run-test-1",
		Payload: &jobpb.Payload{Body: &jobpb.Payload_Text{Text: "assess"}},
		Backend: &jobpb.Backend{
			Kind:    jobpb.Backend_ANTHROPIC_COMPATIBLE,
			BaseUrl: url,
			Model:   "claude-fable-5",
		},
		Framing: &jobpb.Framing{Strategy: jobpb.Framing_WHOLE_OR_ERROR},
	}
}

func TestRunLandsProduced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"model":"m","content":[{"type":"text","text":"out"}],"usage":{"input_tokens":1,"output_tokens":2}}`))
	}))
	defer srv.Close()

	c := Run(context.Background(), specFor(srv.URL))
	if c.GetOutcome() != jobpb.JobCompletion_PRODUCED {
		t.Fatalf("outcome = %s (%s), want PRODUCED", c.GetOutcome(), c.GetReason())
	}
	if c.GetJobId() != "run-test-1" {
		t.Fatalf("job_id = %q", c.GetJobId())
	}
	if c.GetResult().GetText() != "out" {
		t.Fatalf("result = %q", c.GetResult().GetText())
	}
	if c.GetLaneUsed() != jobpb.Lane_LANE_API {
		t.Fatalf("lane = %s", c.GetLaneUsed())
	}
	if c.GetChunks() != 1 {
		t.Fatalf("chunks = %d; whole-or-error is its own single chunk", c.GetChunks())
	}
}

func TestRunLandsFailedOnUnregisteredKind(t *testing.T) {
	spec := specFor("http://127.0.0.1:1")
	spec.Backend.Kind = jobpb.Backend_MLX // admission refuses this upstream; the runner is the backstop
	c := Run(context.Background(), spec)
	if c.GetOutcome() != jobpb.JobCompletion_FAILED {
		t.Fatalf("outcome = %s, want FAILED", c.GetOutcome())
	}
	if !strings.Contains(c.GetReason(), "no implementation") {
		t.Fatalf("reason = %q; a failure names its specific cause", c.GetReason())
	}
	if c.GetResult() != nil {
		t.Fatal("a FAILED completion never fabricates output")
	}
}

func TestRunLandsFailedOnEndpointError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"overloaded"}}`))
	}))
	defer srv.Close()

	c := Run(context.Background(), specFor(srv.URL))
	if c.GetOutcome() != jobpb.JobCompletion_FAILED {
		t.Fatalf("outcome = %s, want FAILED", c.GetOutcome())
	}
	if !strings.Contains(c.GetReason(), "overloaded") {
		t.Fatalf("reason = %q; the endpoint's reason rides the completion", c.GetReason())
	}
}
