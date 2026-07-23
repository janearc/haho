package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/janearc/haho/gen/jobpb"
	"github.com/janearc/haho/internal/hahod/admission"
	"github.com/janearc/haho/internal/hahod/store"
)

// newTestServer wires a real store, real admission, and the real handler
// onto httptest. The only stub is the model endpoint the spec dials.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "hahod.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	srv := New(context.Background(), st, admission.New(admission.Defaults()),
		slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelError})),
		"http://hahod.test")
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(string(p))
	return len(p), nil
}

// stubModel speaks just enough of the messages API for a landing.
func stubModel(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"model": "claude-fable-5-20260101",
			"content": [{"type": "text", "text": "ratify"}],
			"usage": {"input_tokens": 7, "output_tokens": 1}
		}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func postJSON(t *testing.T, url string, body []byte) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	var parsed map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp.StatusCode, parsed
}

func getJSON(t *testing.T, url string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	var parsed map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp.StatusCode, parsed
}

func specJSON(t *testing.T, modelURL string) []byte {
	t.Helper()
	spec := &jobpb.JobSpec{
		JobId:   "ruling-test-1",
		Payload: &jobpb.Payload{Body: &jobpb.Payload_Text{Text: "the assembled ruling prompt"}},
		Backend: &jobpb.Backend{
			Kind:    jobpb.Backend_ANTHROPIC_COMPATIBLE,
			BaseUrl: modelURL,
			Model:   "claude-fable-5",
		},
		Framing:   &jobpb.Framing{Strategy: jobpb.Framing_WHOLE_OR_ERROR},
		Synthesis: &jobpb.Synthesis{Mode: jobpb.Synthesis_NONE},
		Cache:     &jobpb.Cache{Kind: jobpb.Cache_NONE},
	}
	raw, err := protojson.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	return raw
}

// TestNewTaskLand is the skeleton's end-to-end: new session, task with a
// JobSpec, poll status to PRODUCED, read the completion back as protojson.
// This is the RFC's console walkthrough as a test.
func TestNewTaskLand(t *testing.T) {
	hahod := newTestServer(t)
	model := stubModel(t)

	status, created := postJSON(t, hahod.URL+"/sessions", nil)
	if status != http.StatusCreated || created["created"] != true {
		t.Fatalf("new session: %d %v", status, created)
	}
	id := created["id"].(string)

	status, accepted := postJSON(t, fmt.Sprintf("%s/sessions/%s/task", hahod.URL, id), specJSON(t, model.URL))
	if status != http.StatusAccepted || accepted["accepted"] != true {
		t.Fatalf("task: %d %v", status, accepted)
	}

	deadline := time.Now().Add(10 * time.Second)
	var disposition string
	for time.Now().Before(deadline) {
		_, st := getJSON(t, fmt.Sprintf("%s/sessions/%s", hahod.URL, id))
		if st["in_flight"] == false && st["disposition"] != "" {
			disposition = st["disposition"].(string)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if disposition != "PRODUCED" {
		t.Fatalf("disposition = %q, want PRODUCED", disposition)
	}

	resp, err := http.Get(fmt.Sprintf("%s/sessions/%s/completion", hahod.URL, id))
	if err != nil {
		t.Fatalf("GET completion: %v", err)
	}
	defer resp.Body.Close()
	completion := &jobpb.JobCompletion{}
	raw := new(bytes.Buffer)
	if _, err := raw.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read completion: %v", err)
	}
	if err := protojson.Unmarshal(raw.Bytes(), completion); err != nil {
		t.Fatalf("completion is not protojson JobCompletion: %v", err)
	}
	if completion.GetOutcome() != jobpb.JobCompletion_PRODUCED {
		t.Fatalf("outcome = %s, want PRODUCED", completion.GetOutcome())
	}
	if completion.GetResult().GetText() != "ratify" {
		t.Fatalf("result text = %q, want %q", completion.GetResult().GetText(), "ratify")
	}
	if completion.GetModelUsed() != "claude-fable-5-20260101" {
		t.Fatalf("model_used = %q", completion.GetModelUsed())
	}
	if completion.GetLaneUsed() != jobpb.Lane_LANE_API {
		t.Fatalf("lane_used = %s, want LANE_API", completion.GetLaneUsed())
	}
}

func TestTaskRefusalIsLoudAndSpendsNothing(t *testing.T) {
	hahod := newTestServer(t)

	_, created := postJSON(t, hahod.URL+"/sessions", nil)
	id := created["id"].(string)

	spec := &jobpb.JobSpec{
		JobId:   "job-metal",
		Payload: &jobpb.Payload{Body: &jobpb.Payload_Text{Text: "x"}},
		Backend: &jobpb.Backend{Kind: jobpb.Backend_MLX, Model: "whisper-large-v3"},
		Framing: &jobpb.Framing{Strategy: jobpb.Framing_WHOLE_OR_ERROR},
	}
	raw, _ := protojson.Marshal(spec)
	status, refused := postJSON(t, fmt.Sprintf("%s/sessions/%s/task", hahod.URL, id), raw)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", status)
	}
	if refused["refused"] != true || refused["code"] != "lane_not_served" {
		t.Fatalf("refusal = %v", refused)
	}

	// The refusal spent nothing: the session is still quiescent and taskable.
	_, st := getJSON(t, fmt.Sprintf("%s/sessions/%s", hahod.URL, id))
	if st["in_flight"] != false || st["disposition"] != "" {
		t.Fatalf("refused session should stay quiescent, got %v", st)
	}
}

func TestOffContractSpecNeverArrives(t *testing.T) {
	hahod := newTestServer(t)
	_, created := postJSON(t, hahod.URL+"/sessions", nil)
	id := created["id"].(string)

	status, resp := postJSON(t, fmt.Sprintf("%s/sessions/%s/task", hahod.URL, id),
		[]byte(`{"jobId": "x", "deadline": {"budgetMs": 500}}`))
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: an off-contract message never arrives", status)
	}
	if resp["error"] == nil {
		t.Fatalf("expected a loud error body, got %v", resp)
	}
}

func TestSessionIsTaskedExactlyOnce(t *testing.T) {
	hahod := newTestServer(t)
	model := stubModel(t)
	_, created := postJSON(t, hahod.URL+"/sessions", nil)
	id := created["id"].(string)

	if status, _ := postJSON(t, fmt.Sprintf("%s/sessions/%s/task", hahod.URL, id), specJSON(t, model.URL)); status != http.StatusAccepted {
		t.Fatalf("first task: %d", status)
	}
	status, _ := postJSON(t, fmt.Sprintf("%s/sessions/%s/task", hahod.URL, id), specJSON(t, model.URL))
	if status != http.StatusConflict {
		t.Fatalf("second task: %d, want 409", status)
	}
}

func TestUnknownSessionIs404(t *testing.T) {
	hahod := newTestServer(t)
	if status, _ := getJSON(t, hahod.URL+"/sessions/haho-0-nope0"); status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
	if status, _ := postJSON(t, hahod.URL+"/sessions/haho-0-nope0/task", []byte(`{}`)); status != http.StatusNotFound {
		t.Fatalf("task on unknown session: %d, want 404", status)
	}
	if status, _ := getJSON(t, hahod.URL+"/sessions/haho-0-nope0/completion"); status != http.StatusNotFound {
		t.Fatalf("completion of unknown session: %d, want 404", status)
	}
}

func TestCompletionBeforeLandingIs404(t *testing.T) {
	hahod := newTestServer(t)
	_, created := postJSON(t, hahod.URL+"/sessions", nil)
	id := created["id"].(string)
	status, resp := getJSON(t, fmt.Sprintf("%s/sessions/%s/completion", hahod.URL, id))
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: no completion has landed", status)
	}
	if resp["error"] == nil {
		t.Fatalf("expected a loud error body, got %v", resp)
	}
}

func TestHealthAndMetricsServe(t *testing.T) {
	hahod := newTestServer(t)
	postJSON(t, hahod.URL+"/sessions", nil)
	status, health := getJSON(t, hahod.URL+"/health")
	if status != http.StatusOK || health["healthy"] != true {
		t.Fatalf("health: %d %v", status, health)
	}
	resp, err := http.Get(hahod.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET metrics: %v", err)
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	if !bytes.Contains(buf.Bytes(), []byte("hahod_sessions_created_total 1")) {
		t.Fatalf("metrics missing session counter:\n%s", buf.String())
	}
}
