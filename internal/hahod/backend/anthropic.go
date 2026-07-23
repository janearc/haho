package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/janearc/haho/gen/jobpb"
)

// anthropic speaks the messages API for Backend.Kind ANTHROPIC_COMPATIBLE.
// The descriptor carries base_url, model, and token_env (the env var NAME of
// the credential — the value is read here at launch and goes nowhere but
// the request header). params understood by this backend:
//   max_tokens — response budget, default 4096
//   timeout_s  — whole-attempt timeout, default 300; api latency is
//                weather (RFC section 10), so the bound is generous and
//                the requestor can tighten it
type anthropic struct {
	baseURL   string
	model     string
	key       string
	maxTokens int
	client    *http.Client
}

const anthropicVersion = "2023-06-01"

func init() {
	Register(jobpb.Backend_ANTHROPIC_COMPATIBLE, newAnthropic)
}

func newAnthropic(spec *jobpb.JobSpec) (Backend, error) {
	b := spec.GetBackend()
	if b.GetBaseUrl() == "" {
		return nil, fmt.Errorf("anthropic: descriptor carries no base_url")
	}
	if b.GetModel() == "" {
		return nil, fmt.Errorf("anthropic: descriptor carries no model")
	}
	key := ""
	if envName := b.GetTokenEnv(); envName != "" {
		key = os.Getenv(envName) // validated non-empty at admission
	}
	maxTokens := 4096
	if raw, ok := b.GetParams()["max_tokens"]; ok {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("anthropic: params.max_tokens %q is not a positive integer", raw)
		}
		maxTokens = n
	}
	timeout := 300 * time.Second
	if raw, ok := b.GetParams()["timeout_s"]; ok {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("anthropic: params.timeout_s %q is not a positive integer", raw)
		}
		timeout = time.Duration(n) * time.Second
	}
	return &anthropic{
		baseURL:   b.GetBaseUrl(),
		model:     b.GetModel(),
		key:       key,
		maxTokens: maxTokens,
		client:    &http.Client{Timeout: timeout},
	}, nil
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicResponse struct {
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int32 `json:"input_tokens"`
		OutputTokens int32 `json:"output_tokens"`
	} `json:"usage"`
}

func (a *anthropic) Run(ctx context.Context, spec *jobpb.JobSpec) (*Result, error) {
	req := anthropicRequest{Model: a.model, MaxTokens: a.maxTokens}
	switch body := spec.GetPayload().GetBody().(type) {
	case *jobpb.Payload_Text:
		req.Messages = []anthropicMessage{{Role: "user", Content: body.Text}}
	case *jobpb.Payload_Messages:
		for _, m := range body.Messages.GetMessages() {
			// The messages API takes system as a top-level field, not a role.
			if m.GetRole() == "system" {
				req.System = m.GetContent()
				continue
			}
			req.Messages = append(req.Messages, anthropicMessage{Role: m.GetRole(), Content: m.GetContent()})
		}
	default:
		return nil, fmt.Errorf("anthropic: unservable payload shape") // admission refuses this first
	}
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("anthropic: payload carries no messages")
	}

	raw, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/messages", bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("anthropic: build request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	if a.key != "" {
		httpReq.Header.Set("x-api-key", a.key)
	}

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}
	defer resp.Body.Close()
	// The error body is diagnostic gold and small; the cap keeps a
	// misbehaving endpoint from stuffing a completion record.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("anthropic: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic: %s: %s", resp.Status, truncate(string(body), 512))
	}

	var parsed anthropicResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("anthropic: unmarshal response: %w", err)
	}
	var text strings.Builder
	for _, c := range parsed.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	model := parsed.Model
	if model == "" {
		model = a.model
	}
	return &Result{
		Text:      text.String(),
		ModelUsed: model,
		TokensIn:  parsed.Usage.InputTokens,
		TokensOut: parsed.Usage.OutputTokens,
	}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
