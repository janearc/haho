package model

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/janearc/haho/internal/filter"
)

func TestNewProvider_Local(t *testing.T) {
	p, err := NewProvider("local", "http://localhost:11434", "llama3", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := p.(*LocalProvider); !ok {
		t.Fatalf("expected *LocalProvider, got %T", p)
	}
}

func TestNewProvider_Opus(t *testing.T) {
	p, err := NewProvider("opus", "https://api.anthropic.com", "claude-sonnet-4-20250514", "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := p.(*OpusProvider); !ok {
		t.Fatalf("expected *OpusProvider, got %T", p)
	}
}

func TestNewProvider_DeepSeek(t *testing.T) {
	p, err := NewProvider("deepseek", "https://api.deepseek.com", "deepseek-chat", "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := p.(*DeepSeekProvider); !ok {
		t.Fatalf("expected *DeepSeekProvider, got %T", p)
	}
}

func TestNewProvider_Unknown(t *testing.T) {
	_, err := NewProvider("unknown", "", "", "")
	if err == nil {
		t.Fatal("expected error for unknown provider, got nil")
	}
}

func TestLocalProvider_Generate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("expected path /api/chat, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var reqBody ollamaChatRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if reqBody.Model != "llama3" {
			t.Errorf("expected model llama3, got %s", reqBody.Model)
		}
		if len(reqBody.Messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(reqBody.Messages))
		}
		if reqBody.Messages[0].Role != "user" {
			t.Errorf("expected role user, got %s", reqBody.Messages[0].Role)
		}
		if reqBody.Stream {
			t.Error("expected stream to be false")
		}

		resp := ollamaChatResponse{
			Message:   ollamaChatMessage{Role: "assistant", Content: "Hello from Ollama"},
			Model:     "llama3",
			EvalCount: 42,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := NewLocalProvider(srv.URL, "llama3")
	resp, err := p.Generate(context.Background(), &Request{
		Messages: []filter.Message{
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello from Ollama" {
		t.Errorf("expected content 'Hello from Ollama', got %q", resp.Content)
	}
	if resp.TokensUsed != 42 {
		t.Errorf("expected 42 tokens, got %d", resp.TokensUsed)
	}
	if resp.Model != "llama3" {
		t.Errorf("expected model llama3, got %s", resp.Model)
	}
}

func TestLocalProvider_Generate_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer srv.Close()

	p := NewLocalProvider(srv.URL, "llama3")
	_, err := p.Generate(context.Background(), &Request{
		Messages: []filter.Message{
			{Role: "user", Content: "hello"},
		},
	})
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

func TestLocalProvider_DefaultEndpoint(t *testing.T) {
	p := NewLocalProvider("", "")
	if p.endpoint != "http://localhost:11434" {
		t.Errorf("expected default endpoint http://localhost:11434, got %s", p.endpoint)
	}
	if p.modelName != "llama3" {
		t.Errorf("expected default model llama3, got %s", p.modelName)
	}
}

func TestOpusProvider_Generate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("expected path /v1/messages, got %s", r.URL.Path)
		}

		var reqBody anthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if reqBody.Model != "claude-sonnet-4-20250514" {
			t.Errorf("expected model claude-sonnet-4-20250514, got %s", reqBody.Model)
		}
		if reqBody.MaxTokens != 4096 {
			t.Errorf("expected max_tokens 4096, got %d", reqBody.MaxTokens)
		}
		if len(reqBody.Messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(reqBody.Messages))
		}

		resp := anthropicResponse{
			Content: []anthropicContentBlock{
				{Type: "text", Text: "Hello from Claude"},
			},
			Usage: anthropicUsage{InputTokens: 10, OutputTokens: 20},
			Model: "claude-sonnet-4-20250514",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := NewOpusProvider(srv.URL, "claude-sonnet-4-20250514", "test-key")
	resp, err := p.Generate(context.Background(), &Request{
		Messages: []filter.Message{
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello from Claude" {
		t.Errorf("expected content 'Hello from Claude', got %q", resp.Content)
	}
	if resp.TokensUsed != 30 {
		t.Errorf("expected 30 tokens, got %d", resp.TokensUsed)
	}
	if resp.Model != "claude-sonnet-4-20250514" {
		t.Errorf("expected model claude-sonnet-4-20250514, got %s", resp.Model)
	}
}

func TestOpusProvider_Generate_SystemMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody anthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		// System message should be in the system parameter, not in messages
		if reqBody.System != "You are a helpful assistant" {
			t.Errorf("expected system parameter 'You are a helpful assistant', got %q", reqBody.System)
		}
		// Only the user message should be in messages
		if len(reqBody.Messages) != 1 {
			t.Fatalf("expected 1 message (system excluded), got %d", len(reqBody.Messages))
		}
		if reqBody.Messages[0].Role != "user" {
			t.Errorf("expected role user, got %s", reqBody.Messages[0].Role)
		}

		resp := anthropicResponse{
			Content: []anthropicContentBlock{
				{Type: "text", Text: "Hello"},
			},
			Usage: anthropicUsage{InputTokens: 5, OutputTokens: 5},
			Model: "claude-sonnet-4-20250514",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := NewOpusProvider(srv.URL, "claude-sonnet-4-20250514", "test-key")
	_, err := p.Generate(context.Background(), &Request{
		Messages: []filter.Message{
			{Role: "system", Content: "You are a helpful assistant"},
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpusProvider_Headers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("x-api-key")
		if apiKey != "test-api-key" {
			t.Errorf("expected x-api-key 'test-api-key', got %q", apiKey)
		}
		version := r.Header.Get("anthropic-version")
		if version != "2023-06-01" {
			t.Errorf("expected anthropic-version '2023-06-01', got %q", version)
		}
		contentType := r.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("expected Content-Type 'application/json', got %q", contentType)
		}

		resp := anthropicResponse{
			Content: []anthropicContentBlock{
				{Type: "text", Text: "ok"},
			},
			Usage: anthropicUsage{InputTokens: 1, OutputTokens: 1},
			Model: "claude-sonnet-4-20250514",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := NewOpusProvider(srv.URL, "claude-sonnet-4-20250514", "test-api-key")
	_, err := p.Generate(context.Background(), &Request{
		Messages: []filter.Message{
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeepSeekProvider_Generate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("expected path /v1/chat/completions, got %s", r.URL.Path)
		}

		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-ds-key" {
			t.Errorf("expected Authorization 'Bearer test-ds-key', got %q", auth)
		}

		var reqBody openaiRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if reqBody.Model != "deepseek-chat" {
			t.Errorf("expected model deepseek-chat, got %s", reqBody.Model)
		}
		if len(reqBody.Messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(reqBody.Messages))
		}

		resp := openaiResponse{
			Choices: []openaiChoice{
				{Message: openaiMessage{Role: "assistant", Content: "Hello from DeepSeek"}},
			},
			Usage: openaiUsage{TotalTokens: 55},
			Model: "deepseek-chat",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := NewDeepSeekProvider(srv.URL, "deepseek-chat", "test-ds-key")
	resp, err := p.Generate(context.Background(), &Request{
		Messages: []filter.Message{
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello from DeepSeek" {
		t.Errorf("expected content 'Hello from DeepSeek', got %q", resp.Content)
	}
	if resp.TokensUsed != 55 {
		t.Errorf("expected 55 tokens, got %d", resp.TokensUsed)
	}
	if resp.Model != "deepseek-chat" {
		t.Errorf("expected model deepseek-chat, got %s", resp.Model)
	}
}
