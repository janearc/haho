package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/janearc/haho/internal/config"
	"github.com/janearc/haho/internal/harness"
	"github.com/janearc/haho/internal/model"
)

type mockProvider struct {
	name     string
	response *model.Response
	err      error
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) Generate(ctx context.Context, req *model.Request) (*model.Response, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}
func (m *mockProvider) Close() error { return nil }

func testSetup(t *testing.T) (*Server, *mockProvider) {
	t.Helper()
	mp := &mockProvider{
		name: "mock",
		response: &model.Response{
			Content:    "test response",
			TokensUsed: 10,
			Model:      "test-model",
		},
	}
	cfg := &config.Config{
		ListenAddr: ":0",
		Model: config.ModelConfig{
			Provider:  "local",
			Endpoint:  "http://localhost:11434",
			ModelName: "test-model",
		},
		SurrealDB: config.SurrealConfig{
			Discovery: "static",
			Host:      "localhost",
			Port:      8000,
		},
		Filters: config.FiltersConfig{
			Input:  []config.FilterEntry{{Name: "passthrough", Type: "PASSTHROUGH"}},
			Output: []config.FilterEntry{{Name: "passthrough", Type: "PASSTHROUGH"}},
		},
		Cache: config.CacheConfig{Enabled: false},
	}
	h, err := harness.New(cfg, harness.WithProvider(mp))
	if err != nil {
		t.Fatal(err)
	}
	srv := New(h, cfg)
	return srv, mp
}

func TestHandleSubmit(t *testing.T) {
	srv, _ := testSetup(t)
	reqBody := `{"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/submit", bytes.NewBufferString(reqBody))
	rec := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp SubmitResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Content != "test response" {
		t.Errorf("expected 'test response', got %q", resp.Content)
	}
}

func TestHandleSubmit_InvalidBody(t *testing.T) {
	srv, _ := testSetup(t)
	req := httptest.NewRequest("POST", "/v1/submit", bytes.NewBufferString(`{invalid`))
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleHealth(t *testing.T) {
	srv, _ := testSetup(t)
	req := httptest.NewRequest("GET", "/v1/health", nil)
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.State != "idle" {
		t.Errorf("expected idle state, got %s", resp.State)
	}
	if resp.ModelName != "mock/test-model" {
		t.Errorf("expected model name mock/test-model, got %s", resp.ModelName)
	}
}

func TestHandleConfig(t *testing.T) {
	srv, _ := testSetup(t)
	req := httptest.NewRequest("GET", "/v1/config", nil)
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var cfg config.Config
	if err := json.NewDecoder(rec.Body).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != ":0" {
		t.Errorf("expected :0, got %s", cfg.ListenAddr)
	}
}

func TestHandleSetFilters(t *testing.T) {
	srv, _ := testSetup(t)
	reqBody := `{"stage":"output","filters":[{"name":"passthrough","type":"PASSTHROUGH"}]}`
	req := httptest.NewRequest("POST", "/v1/filters", bytes.NewBufferString(reqBody))
	rec := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp SetFilterResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success {
		t.Errorf("expected success")
	}
}

func TestHandleSetFilters_InvalidStage(t *testing.T) {
	srv, _ := testSetup(t)
	reqBody := `{"stage":"invalid","filters":[]}`
	req := httptest.NewRequest("POST", "/v1/filters", bytes.NewBufferString(reqBody))
	rec := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}
