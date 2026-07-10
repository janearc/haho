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

// DeepSeekProvider implements Provider using the OpenAI-compatible chat completions API.
type DeepSeekProvider struct {
	endpoint  string
	modelName string
	apiKey    string
	client    *http.Client
}

// NewDeepSeekProvider creates a new DeepSeek-backed provider.
func NewDeepSeekProvider(endpoint, modelName, apiKey string) *DeepSeekProvider {
	if endpoint == "" {
		endpoint = "https://api.deepseek.com"
	}
	if modelName == "" {
		modelName = "deepseek-chat"
	}
	return &DeepSeekProvider{
		endpoint:  endpoint,
		modelName: modelName,
		apiKey:    apiKey,
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// Name returns "deepseek".
func (p *DeepSeekProvider) Name() string { return "deepseek" }

// Generate sends a chat completion request to the DeepSeek API.
func (p *DeepSeekProvider) Generate(ctx context.Context, req *Request) (*Response, error) {
	model := p.modelName
	if req.Model != "" {
		model = req.Model
	}

	msgs := make([]openaiMessage, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = openaiMessage{
			Role:    m.Role,
			Content: m.Content,
		}
	}

	body := openaiRequest{
		Model:    model,
		Messages: msgs,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("deepseek: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("deepseek: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("deepseek: send request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("deepseek: read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("deepseek: unexpected status %d: %s", httpResp.StatusCode, string(respBody))
	}

	var openaiResp openaiResponse
	if err := json.Unmarshal(respBody, &openaiResp); err != nil {
		return nil, fmt.Errorf("deepseek: parse response: %w", err)
	}

	if len(openaiResp.Choices) == 0 {
		return nil, fmt.Errorf("deepseek: no choices in response")
	}

	return &Response{
		Content:    openaiResp.Choices[0].Message.Content,
		TokensUsed: openaiResp.Usage.TotalTokens,
		Model:      openaiResp.Model,
	}, nil
}

// Close is a no-op for the deepseek provider.
func (p *DeepSeekProvider) Close() error { return nil }

// OpenAI-compatible API types (unexported).

type openaiRequest struct {
	Model    string          `json:"model"`
	Messages []openaiMessage `json:"messages"`
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiResponse struct {
	Choices []openaiChoice `json:"choices"`
	Usage   openaiUsage    `json:"usage"`
	Model   string         `json:"model"`
}

type openaiChoice struct {
	Message openaiMessage `json:"message"`
}

type openaiUsage struct {
	TotalTokens int `json:"total_tokens"`
}
