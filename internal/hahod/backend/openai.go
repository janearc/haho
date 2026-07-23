package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/janearc/haho/gen/jobpb"
)

// openai speaks the /api/chat shape for Backend.Kind OPENAI_COMPATIBLE —
// the contract's words: "the /api/chat shape; local ollama too". Local
// ollama is the reason this door exists (the paid api lane is the last
// resort, not the default), so no credential is required: token_env is
// optional and becomes a bearer header only when the spec names one.
// A strict OpenAI /v1/chat/completions dialect, if ever needed, is a
// param or a later kind — surfaced here, not smuggled. params understood:
//
//	max_tokens — response budget, sent as options.num_predict; absent
//	             means the endpoint's own default
//	timeout_s  — whole-attempt timeout, default 300, ceiling 3600
type openai struct {
	baseURL string
	model   string
	key     string
	opts    map[string]any
	client  *http.Client
}

func init() {
	Register(jobpb.Backend_OPENAI_COMPATIBLE, newOpenAI)
}

func newOpenAI(spec *jobpb.JobSpec) (Backend, error) {
	b := spec.GetBackend()
	if b.GetBaseUrl() == "" {
		return nil, fmt.Errorf("openai: descriptor carries no base_url")
	}
	if b.GetModel() == "" {
		return nil, fmt.Errorf("openai: descriptor carries no model")
	}
	key := ""
	if envName := b.GetTokenEnv(); envName != "" {
		key = os.Getenv(envName) // validated non-empty at admission
	}
	opts := map[string]any{}
	if raw, ok := b.GetParams()["max_tokens"]; ok {
		n, err := positiveInt(raw)
		if err != nil {
			return nil, fmt.Errorf("openai: params.max_tokens %q is not a positive integer", raw)
		}
		opts["num_predict"] = n
	}
	timeout := 300 * time.Second
	if raw, ok := b.GetParams()["timeout_s"]; ok {
		n, err := positiveInt(raw)
		if err != nil {
			return nil, fmt.Errorf("openai: params.timeout_s %q is not a positive integer", raw)
		}
		if n > maxTimeoutSeconds {
			return nil, fmt.Errorf("openai: params.timeout_s %d exceeds the %ds ceiling", n, maxTimeoutSeconds)
		}
		timeout = time.Duration(n) * time.Second
	}
	return &openai{
		baseURL: b.GetBaseUrl(),
		model:   b.GetModel(),
		key:     key,
		opts:    opts,
		client:  &http.Client{Timeout: timeout},
	}, nil
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string         `json:"model"`
	Messages []chatMessage  `json:"messages"`
	Stream   bool           `json:"stream"` // always false: haho is packets, not streaming
	Options  map[string]any `json:"options,omitempty"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	// Pointers so an omitted count is distinguishable from a genuine
	// zero: absent lands as -1, never a fabricated 0.
	PromptEvalCount *int32 `json:"prompt_eval_count"`
	EvalCount       *int32 `json:"eval_count"`
}

func (o *openai) Run(ctx context.Context, spec *jobpb.JobSpec) (*Result, error) {
	req := chatRequest{Model: o.model, Stream: false, Options: o.opts}
	switch body := spec.GetPayload().GetBody().(type) {
	case *jobpb.Payload_Text:
		req.Messages = []chatMessage{{Role: "user", Content: body.Text}}
	case *jobpb.Payload_Messages:
		// The /api/chat shape takes system as an ordinary role; messages
		// pass through as written.
		for _, m := range body.Messages.GetMessages() {
			req.Messages = append(req.Messages, chatMessage{Role: m.GetRole(), Content: m.GetContent()})
		}
	default:
		return nil, fmt.Errorf("openai: unservable payload shape") // admission refuses this first
	}
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("openai: payload carries no messages")
	}

	raw, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/chat", bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("openai: build request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	if o.key != "" {
		httpReq.Header.Set("authorization", "Bearer "+o.key)
	}

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("openai: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai: %s: %s", resp.Status, truncate(string(body), 512))
	}
	if len(body) >= maxResponseBytes {
		return nil, fmt.Errorf("openai: response body reached the %d-byte cap; refusing to parse a possibly-truncated reply", maxResponseBytes)
	}

	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("openai: unmarshal response: %w", err)
	}
	model := parsed.Model
	if model == "" {
		model = o.model
	}
	res := &Result{Text: parsed.Message.Content, ModelUsed: model, TokensIn: -1, TokensOut: -1}
	if parsed.PromptEvalCount != nil {
		res.TokensIn = *parsed.PromptEvalCount
	}
	if parsed.EvalCount != nil {
		res.TokensOut = *parsed.EvalCount
	}
	return res, nil
}
