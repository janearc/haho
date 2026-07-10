package model

import (
	"context"
	"fmt"

	"github.com/janearc/haho/internal/filter"
)

// Request represents an LLM generation request.
type Request struct {
	Messages []filter.Message       `json:"messages"`
	Model    string                 `json:"model,omitempty"`
	Params   map[string]interface{} `json:"params,omitempty"`
}

// Response represents an LLM generation response.
type Response struct {
	Content    string            `json:"content"`
	TokensUsed int               `json:"tokens_used"`
	Model      string            `json:"model"`
	Cached     bool              `json:"cached"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// Provider generates LLM completions.
type Provider interface {
	Name() string
	Generate(ctx context.Context, req *Request) (*Response, error)
	Close() error
}

// NewProvider creates a provider by name. Supported: "local", "opus", "deepseek".
func NewProvider(name, endpoint, modelName, apiKey string) (Provider, error) {
	switch name {
	case "local":
		return NewLocalProvider(endpoint, modelName), nil
	case "opus":
		return NewOpusProvider(endpoint, modelName, apiKey), nil
	case "deepseek":
		return NewDeepSeekProvider(endpoint, modelName, apiKey), nil
	default:
		return nil, fmt.Errorf("unknown provider: %q", name)
	}
}
