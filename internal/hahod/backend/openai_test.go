package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/janearc/haho/gen/commonpb"
	"github.com/janearc/haho/gen/jobpb"
)

func openaiSpec(url string) *jobpb.JobSpec {
	return &jobpb.JobSpec{
		JobId:   "local-1",
		Payload: &jobpb.Payload{Body: &jobpb.Payload_Text{Text: "hello, mesh"}},
		Backend: &jobpb.Backend{
			Kind:    jobpb.Backend_OPENAI_COMPATIBLE,
			BaseUrl: url,
			Model:   "mistral-24b:latest",
		},
	}
}

// chatStub serves just enough of the /api/chat shape, capturing the
// request it saw.
func chatStub(t *testing.T, respBody string) (*httptest.Server, *map[string]any, *http.Header) {
	t.Helper()
	var seen map[string]any
	var hdr http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path = %s, want /api/chat", r.URL.Path)
		}
		hdr = r.Header.Clone()
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen, &hdr
}

const chatOK = `{
  "model": "mistral-24b:latest",
  "message": {"role": "assistant", "content": "hallo, mesh"},
  "prompt_eval_count": 12,
  "eval_count": 6,
  "done": true
}`

func TestOpenAIRunTextPayload(t *testing.T) {
	srv, seen, hdr := chatStub(t, chatOK)
	spec := openaiSpec(srv.URL)
	b, err := New(spec)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	res, err := b.Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Text != "hallo, mesh" {
		t.Fatalf("text = %q", res.Text)
	}
	if res.TokensIn != 12 || res.TokensOut != 6 {
		t.Fatalf("tokens = %d/%d, want 12/6", res.TokensIn, res.TokensOut)
	}
	if (*seen)["stream"] != false {
		t.Fatal("stream must be pinned false: haho is packets, not streaming")
	}
	if got := hdr.Get("authorization"); got != "" {
		t.Fatalf("authorization = %q; no credential was named, none may be sent", got)
	}
}

func TestOpenAISystemRolePassesThrough(t *testing.T) {
	srv, seen, _ := chatStub(t, chatOK)
	spec := openaiSpec(srv.URL)
	spec.Payload = &jobpb.Payload{Body: &jobpb.Payload_Messages{Messages: &jobpb.Messages{
		Messages: []*commonpb.ChatMessage{
			{Role: "system", Content: "wees beleefd"},
			{Role: "user", Content: "hoi"},
		},
	}}}
	b, err := New(spec)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	if _, err := b.Run(context.Background(), spec); err != nil {
		t.Fatalf("run: %v", err)
	}
	msgs := (*seen)["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want 2: /api/chat takes system as an ordinary role", len(msgs))
	}
	if first := msgs[0].(map[string]any); first["role"] != "system" {
		t.Fatalf("first role = %v, want system in place", first["role"])
	}
}

func TestOpenAIAbsentCountsAreMinusOne(t *testing.T) {
	srv, _, _ := chatStub(t, `{"model":"m","message":{"content":"x"},"done":true}`)
	spec := openaiSpec(srv.URL)
	b, err := New(spec)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	res, err := b.Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.TokensIn != -1 || res.TokensOut != -1 {
		t.Fatalf("tokens = %d/%d, want -1/-1: absent is not zero", res.TokensIn, res.TokensOut)
	}
}

func TestOpenAITokenEnvBecomesBearer(t *testing.T) {
	t.Setenv("HAHO_TEST_OPENAI_TOKEN", "tok-value")
	srv, _, hdr := chatStub(t, chatOK)
	spec := openaiSpec(srv.URL)
	spec.Backend.TokenEnv = "HAHO_TEST_OPENAI_TOKEN"
	b, err := New(spec)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	if _, err := b.Run(context.Background(), spec); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := hdr.Get("authorization"); got != "Bearer tok-value" {
		t.Fatalf("authorization = %q", got)
	}
}

func TestOpenAIMaxTokensBecomesNumPredict(t *testing.T) {
	srv, seen, _ := chatStub(t, chatOK)
	spec := openaiSpec(srv.URL)
	spec.Backend.Params = map[string]string{"max_tokens": "128"}
	b, err := New(spec)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	if _, err := b.Run(context.Background(), spec); err != nil {
		t.Fatalf("run: %v", err)
	}
	opts := (*seen)["options"].(map[string]any)
	if opts["num_predict"].(float64) != 128 {
		t.Fatalf("num_predict = %v, want 128", opts["num_predict"])
	}
}

func TestOpenAIConstructorRefusals(t *testing.T) {
	spec := openaiSpec("")
	if _, err := New(spec); err == nil {
		t.Fatal("no base_url must refuse construction")
	}
	spec = openaiSpec("http://127.0.0.1:1")
	spec.Backend.Model = ""
	if _, err := New(spec); err == nil {
		t.Fatal("no model must refuse construction")
	}
	spec = openaiSpec("http://127.0.0.1:1")
	spec.Backend.Params = map[string]string{"timeout_s": "999999"}
	if _, err := New(spec); err == nil {
		t.Fatal("timeout past the ceiling must refuse construction")
	}
}
