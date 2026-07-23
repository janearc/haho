package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/janearc/haho/gen/commonpb"
	"github.com/janearc/haho/gen/jobpb"
)

func specFor(url string) *jobpb.JobSpec {
	return &jobpb.JobSpec{
		JobId:   "test-1",
		Payload: &jobpb.Payload{Body: &jobpb.Payload_Text{Text: "assess this"}},
		Backend: &jobpb.Backend{
			Kind:    jobpb.Backend_ANTHROPIC_COMPATIBLE,
			BaseUrl: url,
			Model:   "claude-fable-5",
		},
	}
}

// stub returns an httptest server speaking just enough of the messages API,
// capturing the request it saw.
func stub(t *testing.T, status int, respBody string) (*httptest.Server, *map[string]any) {
	t.Helper()
	var seen map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %s, want /v1/messages", r.URL.Path)
		}
		if got := r.Header.Get("anthropic-version"); got == "" {
			t.Error("anthropic-version header absent")
		}
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.WriteHeader(status)
		w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

const okResponse = `{
  "model": "claude-fable-5-20260101",
  "content": [{"type": "text", "text": "a "}, {"type": "text", "text": "ruling"}],
  "usage": {"input_tokens": 10, "output_tokens": 5}
}`

func TestRunTextPayload(t *testing.T) {
	srv, seen := stub(t, http.StatusOK, okResponse)
	b, err := New(specFor(srv.URL))
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	res, err := b.Run(context.Background(), specFor(srv.URL))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Text != "a ruling" {
		t.Fatalf("text = %q, want %q (content blocks concatenated)", res.Text, "a ruling")
	}
	if res.ModelUsed != "claude-fable-5-20260101" {
		t.Fatalf("model_used = %q; the completion surfaces the model that actually answered", res.ModelUsed)
	}
	if res.TokensIn != 10 || res.TokensOut != 5 {
		t.Fatalf("tokens = %d/%d, want 10/5", res.TokensIn, res.TokensOut)
	}
	msgs := (*seen)["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("request messages = %d, want 1", len(msgs))
	}
}

func TestRunMessagesPayloadLiftsSystem(t *testing.T) {
	srv, seen := stub(t, http.StatusOK, okResponse)
	spec := specFor(srv.URL)
	spec.Payload = &jobpb.Payload{Body: &jobpb.Payload_Messages{Messages: &jobpb.Messages{
		Messages: []*commonpb.ChatMessage{
			{Role: "system", Content: "you are the judge"},
			{Role: "user", Content: "the diff"},
		},
	}}}
	b, err := New(spec)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	if _, err := b.Run(context.Background(), spec); err != nil {
		t.Fatalf("run: %v", err)
	}
	if (*seen)["system"] != "you are the judge" {
		t.Fatalf("system = %v; a system-role message lifts to the top-level field", (*seen)["system"])
	}
	if msgs := (*seen)["messages"].([]any); len(msgs) != 1 {
		t.Fatalf("messages = %d, want 1 (system lifted out)", len(msgs))
	}
}

func TestRunTokenEnvBecomesHeader(t *testing.T) {
	t.Setenv("HAHO_TEST_TOKEN", "sk-test-value")
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		w.Write([]byte(okResponse))
	}))
	defer srv.Close()
	spec := specFor(srv.URL)
	spec.Backend.TokenEnv = "HAHO_TEST_TOKEN"
	b, err := New(spec)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	if _, err := b.Run(context.Background(), spec); err != nil {
		t.Fatalf("run: %v", err)
	}
	if gotKey != "sk-test-value" {
		t.Fatalf("x-api-key = %q; token_env's value goes to the header and nowhere else", gotKey)
	}
}

func TestRunErrorIsSpecificNeverFabricated(t *testing.T) {
	srv, _ := stub(t, http.StatusBadRequest, `{"error":{"message":"prompt is too long"}}`)
	spec := specFor(srv.URL)
	b, err := New(spec)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	res, err := b.Run(context.Background(), spec)
	if err == nil {
		t.Fatalf("non-2xx must error, got result %+v", res)
	}
	if !strings.Contains(err.Error(), "prompt is too long") {
		t.Fatalf("error %q should carry the endpoint's reason", err)
	}
}

func TestConstructorRefusesBareDescriptor(t *testing.T) {
	spec := specFor("")
	if _, err := New(spec); err == nil {
		t.Fatal("descriptor with no base_url must refuse construction")
	}
	spec = specFor("http://127.0.0.1:1")
	spec.Backend.Model = ""
	if _, err := New(spec); err == nil {
		t.Fatal("descriptor with no model must refuse construction")
	}
}

func TestParamsApplyAndValidate(t *testing.T) {
	srv, seen := stub(t, http.StatusOK, okResponse)
	spec := specFor(srv.URL)
	spec.Backend.Params = map[string]string{"max_tokens": "512", "timeout_s": "10"}
	b, err := New(spec)
	if err != nil {
		t.Fatalf("construct with valid params: %v", err)
	}
	if _, err := b.Run(context.Background(), spec); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := (*seen)["max_tokens"].(float64); got != 512 {
		t.Fatalf("max_tokens = %v, want 512", got)
	}

	for _, bad := range []map[string]string{
		{"max_tokens": "many"},
		{"max_tokens": "0"},
		{"timeout_s": "-1"},
	} {
		spec := specFor(srv.URL)
		spec.Backend.Params = bad
		if _, err := New(spec); err == nil {
			t.Fatalf("params %v must refuse construction", bad)
		}
	}
}

func TestErrorBodyIsTruncated(t *testing.T) {
	long := strings.Repeat("x", 4096)
	srv, _ := stub(t, http.StatusInternalServerError, long)
	spec := specFor(srv.URL)
	b, err := New(spec)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	_, err = b.Run(context.Background(), spec)
	if err == nil {
		t.Fatal("non-2xx must error")
	}
	if len(err.Error()) > 1024 {
		t.Fatalf("error length %d; a misbehaving endpoint must not stuff the record", len(err.Error()))
	}
}

// TestAbsentUsageIsMinusOne pins the seam contract: absent is not zero.
// Pre-fix an omitted usage block landed as a fabricated 0/0.
func TestAbsentUsageIsMinusOne(t *testing.T) {
	srv, _ := stub(t, http.StatusOK, `{"model":"m","content":[{"type":"text","text":"out"}]}`)
	spec := specFor(srv.URL)
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

// TestResponseCapIsNamed pins that a body hitting the cap errors with the
// cap as the reason, not a misleading JSON parse error.
func TestResponseCapIsNamed(t *testing.T) {
	big := strings.Repeat("x", maxResponseBytes+100)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"content":[{"type":"text","text":"` + big + `"}]}`))
	}))
	defer srv.Close()
	spec := specFor(srv.URL)
	b, err := New(spec)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	_, err = b.Run(context.Background(), spec)
	if err == nil {
		t.Fatal("capped body must error")
	}
	if !strings.Contains(err.Error(), "cap") {
		t.Fatalf("error %q must name the cap, not mislabel the truncation", err)
	}
}

func TestTimeoutCeiling(t *testing.T) {
	spec := specFor("http://127.0.0.1:1")
	spec.Backend.Params = map[string]string{"timeout_s": "31536000"}
	if _, err := New(spec); err == nil {
		t.Fatal("a year-long timeout must refuse construction: a lane slot is not parkable")
	}
	spec.Backend.Params = map[string]string{"timeout_s": "3600"}
	if _, err := New(spec); err != nil {
		t.Fatalf("the ceiling itself must construct: %v", err)
	}
}

func TestRegistryRefusesUnknownKind(t *testing.T) {
	spec := specFor("http://127.0.0.1:1")
	spec.Backend.Kind = jobpb.Backend_MLX
	if _, err := New(spec); err == nil {
		t.Fatal("unregistered kind must error, not improvise")
	}
}
