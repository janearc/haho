package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OpusProvider implements Provider using the Anthropic Messages API.
type OpusProvider struct {
	endpoint  string
	modelName string
	apiKey    string
	client    *http.Client
}

// NewOpusProvider creates a new Anthropic-backed provider.
func NewOpusProvider(endpoint, modelName, apiKey string) *OpusProvider {
	if endpoint == "" {
		endpoint = "https://api.anthropic.com"
	}
	if modelName == "" {
		modelName = "claude-opus-4-8"
	}
	return &OpusProvider{
		endpoint:  endpoint,
		modelName: modelName,
		apiKey:    apiKey,
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// Name returns "opus".
func (p *OpusProvider) Name() string { return "opus" }

// Generate sends a chat completion request to the Anthropic Messages API.
func (p *OpusProvider) Generate(ctx context.Context, req *Request) (*Response, error) {
	model := p.modelName
	if req.Model != "" {
		model = req.Model
	}

	var systemText string
	var msgs []anthropicMessage
	for _, m := range req.Messages {
		if m.Role == "system" {
			systemText = m.Content
			continue
		}
		msgs = append(msgs, anthropicMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	body := anthropicRequest{
		Model:     model,
		MaxTokens: 4096,
		Messages:  msgs,
	}
	if systemText != "" {
		body.System = systemText
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("opus: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("opus: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("opus: send request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("opus: read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("opus: unexpected status %d: %s", httpResp.StatusCode, string(respBody))
	}

	var anthropicResp anthropicResponse
	if err := json.Unmarshal(respBody, &anthropicResp); err != nil {
		return nil, fmt.Errorf("opus: parse response: %w", err)
	}

	var content string
	for _, block := range anthropicResp.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}

	return &Response{
		Content:    content,
		TokensUsed: anthropicResp.Usage.InputTokens + anthropicResp.Usage.OutputTokens,
		Model:      anthropicResp.Model,
	}, nil
}

// Close is a no-op for the opus provider.
func (p *OpusProvider) Close() error { return nil }

// Anthropic API types (unexported).

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []anthropicContentBlock `json:"content"`
	Usage   anthropicUsage          `json:"usage"`
	Model   string                  `json:"model"`
}

type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}
