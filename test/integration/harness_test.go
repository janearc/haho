//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/janearc/haho/internal/config"
	"github.com/janearc/haho/internal/filter"
	"github.com/janearc/haho/internal/harness"
	"github.com/janearc/haho/internal/model"
	"github.com/janearc/haho/internal/server"
)

// mockProvider satisfies model.Provider without calling a real LLM.
type mockProvider struct {
	response *model.Response
}

func (m *mockProvider) Name() string { return "mock" }

func (m *mockProvider) Generate(ctx context.Context, req *model.Request) (*model.Response, error) {
	return m.response, nil
}

func (m *mockProvider) Close() error { return nil }

// testConfig loads config from testdata or falls back to an inline default.
func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfgPath := filepath.Join("testdata", "config.json")
	if cfg, err := config.Load(cfgPath); err == nil {
		return cfg
	}
	return &config.Config{
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
}

// waitForServer polls the health endpoint until it responds 200 or timeout.
func waitForServer(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	url := fmt.Sprintf("http://%s/v1/health", addr)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server at %s did not become ready within %v", addr, timeout)
}

// submitRequest POSTs a submit request and returns the raw response.
func submitRequest(addr string, messages []filter.Message) (*http.Response, error) {
	body := map[string]interface{}{
		"messages": messages,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	url := fmt.Sprintf("http://%s/v1/submit", addr)
	return http.Post(url, "application/json", bytes.NewReader(data))
}

// startTestServer creates a harness+server with the mock provider and starts serving.
// Returns the server (for addr/stop) and a cancel func for the context.
func startTestServer(t *testing.T, mockResp *model.Response) (*server.Server, context.CancelFunc) {
	t.Helper()
	cfg := testConfig(t)

	mock := &mockProvider{response: mockResp}
	h, err := harness.New(cfg, harness.WithProvider(mock))
	if err != nil {
		t.Fatalf("creating harness: %v", err)
	}

	srv := server.New(h, cfg)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		if err := srv.ListenAndServe(ctx); err != nil {
			// Context cancellation is expected during shutdown.
			if ctx.Err() == nil {
				t.Errorf("ListenAndServe: %v", err)
			}
		}
	}()

	// Wait for the server to bind its port.
	time.Sleep(100 * time.Millisecond)
	waitForServer(t, srv.Addr(), 5*time.Second)

	t.Cleanup(func() {
		cancel()
		srv.Stop()
		h.Close()
	})

	return srv, cancel
}

func TestIntegration_SubmitEndpoint(t *testing.T) {
	expected := &model.Response{
		Content: "hello from mock",
	}
	srv, _ := startTestServer(t, expected)

	msgs := []filter.Message{{Role: "user", Content: "hello"}}
	resp, err := submitRequest(srv.Addr(), msgs)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	content, ok := result["content"].(string)
	if !ok || content != "hello from mock" {
		t.Errorf("expected content %q, got %v", "hello from mock", result["content"])
	}
}

func TestIntegration_HealthEndpoint(t *testing.T) {
	srv, _ := startTestServer(t, &model.Response{Content: "ok"})

	url := fmt.Sprintf("http://%s/v1/health", srv.Addr())
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	state, ok := result["state"].(string)
	if !ok || state != "idle" {
		t.Errorf("expected state 'idle', got %v", result["state"])
	}
}

func TestIntegration_ConfigEndpoint(t *testing.T) {
	srv, _ := startTestServer(t, &model.Response{Content: "ok"})

	url := fmt.Sprintf("http://%s/v1/config", srv.Addr())
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	listenAddr, ok := result["listen_addr"].(string)
	if !ok || listenAddr == "" {
		t.Errorf("expected non-empty listen_addr, got %v", result["listen_addr"])
	}
}

func TestIntegration_SetFiltersEndpoint(t *testing.T) {
	srv, _ := startTestServer(t, &model.Response{Content: "filtered response"})

	// Update output filters to include a logger.
	filterPayload := map[string]interface{}{
		"stage": "output",
		"filters": []map[string]string{
			{"name": "logger", "type": "LOGGER"},
		},
	}
	data, _ := json.Marshal(filterPayload)
	url := fmt.Sprintf("http://%s/v1/filters", srv.Addr())
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("set filters: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Submit should still work after filter change.
	msgs := []filter.Message{{Role: "user", Content: "test after filter"}}
	submitResp, err := submitRequest(srv.Addr(), msgs)
	if err != nil {
		t.Fatalf("submit after filter change: %v", err)
	}
	defer submitResp.Body.Close()

	if submitResp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 after filter change, got %d", submitResp.StatusCode)
	}
}

func TestIntegration_SubmitThenHealth(t *testing.T) {
	srv, _ := startTestServer(t, &model.Response{Content: "done"})

	// Submit a request.
	msgs := []filter.Message{{Role: "user", Content: "work"}}
	resp, err := submitRequest(srv.Addr(), msgs)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// After submit completes, health should be idle.
	url := fmt.Sprintf("http://%s/v1/health", srv.Addr())
	healthResp, err := http.Get(url)
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	defer healthResp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(healthResp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	state, ok := result["state"].(string)
	if !ok || state != "idle" {
		t.Errorf("expected state 'idle' after submit, got %v", result["state"])
	}
}

func TestIntegration_GracefulShutdown(t *testing.T) {
	cfg := testConfig(t)
	mock := &mockProvider{response: &model.Response{Content: "bye"}}

	h, err := harness.New(cfg, harness.WithProvider(mock))
	if err != nil {
		t.Fatalf("creating harness: %v", err)
	}

	srv := server.New(h, cfg)
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe(ctx)
	}()

	time.Sleep(100 * time.Millisecond)
	waitForServer(t, srv.Addr(), 5*time.Second)

	// Cancel context to trigger graceful shutdown.
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Logf("ListenAndServe returned (expected on shutdown): %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not shut down within 10 seconds")
	}

	srv.Stop()
	h.Close()
}

// Verify testdata config file exists and is loadable.
func TestIntegration_TestdataConfig(t *testing.T) {
	cfgPath := filepath.Join("testdata", "config.json")
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		t.Skip("testdata/config.json not found")
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("loading testdata config: %v", err)
	}

	if cfg.ListenAddr != ":0" {
		t.Errorf("expected listen_addr ':0', got %q", cfg.ListenAddr)
	}
	if cfg.Model.Provider != "local" {
		t.Errorf("expected provider 'local', got %q", cfg.Model.Provider)
	}
}
