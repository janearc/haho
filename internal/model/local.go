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

// LocalProvider implements Provider using the Ollama HTTP API.
type LocalProvider struct {
	endpoint  string
	modelName string
	client    *http.Client
}

// NewLocalProvider creates a new Ollama-backed provider.
func NewLocalProvider(endpoint, modelName string) *LocalProvider {
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}
	if modelName == "" {
		modelName = "llama3"
	}
	return &LocalProvider{
		endpoint:  endpoint,
		modelName: modelName,
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// Name returns "local".
func (p *LocalProvider) Name() string { return "local" }

// Generate sends a chat completion request to the Ollama API.
func (p *LocalProvider) Generate(ctx context.Context, req *Request) (*Response, error) {
	model := p.modelName
	if req.Model != "" {
		model = req.Model
	}

	msgs := make([]ollamaChatMessage, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = ollamaChatMessage{
			Role:    m.Role,
			Content: m.Content,
		}
	}

	body := ollamaChatRequest{
		Model:    model,
		Messages: msgs,
		Stream:   false,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("local: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("local: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("local: send request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("local: read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("local: unexpected status %d: %s", httpResp.StatusCode, string(respBody))
	}

	var ollamaResp ollamaChatResponse
	if err := json.Unmarshal(respBody, &ollamaResp); err != nil {
		return nil, fmt.Errorf("local: parse response: %w", err)
	}

	return &Response{
		Content:    ollamaResp.Message.Content,
		TokensUsed: ollamaResp.EvalCount,
		Model:      ollamaResp.Model,
	}, nil
}

// Close is a no-op for the local provider.
func (p *LocalProvider) Close() error { return nil }

// Ollama API types (unexported).

type ollamaChatRequest struct {
	Model    string              `json:"model"`
	Messages []ollamaChatMessage `json:"messages"`
	Stream   bool                `json:"stream"`
}

type ollamaChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatResponse struct {
	Message       ollamaChatMessage `json:"message"`
	Model         string            `json:"model"`
	EvalCount     int               `json:"eval_count"`
	TotalDuration int64             `json:"total_duration"`
}
