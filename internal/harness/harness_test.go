package harness

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/janearc/haho/internal/config"
	"github.com/janearc/haho/internal/filter"
	"github.com/janearc/haho/internal/health"
	"github.com/janearc/haho/internal/model"
	"github.com/janearc/haho/internal/rag"
)

type mockProvider struct {
	name     string
	response *model.Response
	err      error
	lastReq  *model.Request
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) Generate(ctx context.Context, req *model.Request) (*model.Response, error) {
	m.lastReq = req
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}
func (m *mockProvider) Close() error { return nil }

type mockCache struct {
	data    map[string]*model.Response
	setKeys []string
}

func newMockCache() *mockCache { return &mockCache{data: make(map[string]*model.Response)} }
func (c *mockCache) Get(ctx context.Context, key string) (*model.Response, bool, error) {
	resp, ok := c.data[key]
	return resp, ok, nil
}
func (c *mockCache) Set(ctx context.Context, key string, resp *model.Response, ttl time.Duration) error {
	c.data[key] = resp
	c.setKeys = append(c.setKeys, key)
	return nil
}

type mockStore struct {
	docs    []rag.Document
	results []rag.SearchResult
}

func (s *mockStore) Index(ctx context.Context, docs []rag.Document) error {
	s.docs = append(s.docs, docs...)
	return nil
}
func (s *mockStore) Search(ctx context.Context, query string, limit int) ([]rag.SearchResult, error) {
	return s.results, nil
}
func (s *mockStore) Close() error { return nil }

func testConfig() *config.Config {
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

func TestNew_WithProvider(t *testing.T) {
	mp := &mockProvider{name: "mock"}
	cfg := testConfig()
	h, err := New(cfg, WithProvider(mp))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.provider != mp {
		t.Errorf("expected provider to be mockProvider")
	}
}

func TestNew_WithAllOptions(t *testing.T) {
	mp := &mockProvider{}
	mc := newMockCache()
	ms := &mockStore{}
	cfg := testConfig()
	h, err := New(cfg, WithProvider(mp), WithCache(mc), WithStore(ms))
	if err != nil {
		t.Fatal(err)
	}
	if h.provider != mp || h.cache != mc || h.store != ms {
		t.Errorf("options not set correctly")
	}
}

func TestSubmit_BasicFlow(t *testing.T) {
	mp := &mockProvider{
		response: &model.Response{Content: "hello world", Model: "test-model"},
	}
	h, err := New(testConfig(), WithProvider(mp))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := h.Submit(context.Background(), []filter.Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "hello world" {
		t.Errorf("expected 'hello world', got %q", resp.Content)
	}
}

func TestSubmit_SetsHealthState(t *testing.T) {
	mp := &mockProvider{response: &model.Response{}}
	h, err := New(testConfig(), WithProvider(mp))
	if err != nil {
		t.Fatal(err)
	}
	if h.Health().State != health.StateIdle {
		t.Errorf("expected idle initially")
	}
	_, _ = h.Submit(context.Background(), []filter.Message{{Content: "hi"}})
	// Since Submit is synchronous, it should be idle again
	if h.Health().State != health.StateIdle {
		t.Errorf("expected idle after submit")
	}
}

func TestSubmit_CacheHit(t *testing.T) {
	mc := newMockCache()
	msgs := []filter.Message{{Role: "user", Content: "hi"}}
	key := rag.CacheKey(msgs, "test-model")
	mc.data[key] = &model.Response{Content: "cached hello", Cached: true}

	mp := &mockProvider{response: &model.Response{Content: "mocked hello"}} // should not be hit
	h, err := New(testConfig(), WithProvider(mp), WithCache(mc))
	if err != nil {
		t.Fatal(err)
	}

	resp, err := h.Submit(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "cached hello" || !resp.Cached {
		t.Errorf("expected cache hit")
	}
	if mp.lastReq != nil {
		t.Errorf("provider should not have been called")
	}
}

func TestSubmit_CacheMiss_ThenStores(t *testing.T) {
	mc := newMockCache()
	mp := &mockProvider{response: &model.Response{Content: "mock response"}}
	h, err := New(testConfig(), WithProvider(mp), WithCache(mc))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = h.Submit(context.Background(), []filter.Message{{Role: "user", Content: "hi"}})
	if len(mc.setKeys) != 1 {
		t.Errorf("expected 1 key in cache setKeys, got %d", len(mc.setKeys))
	}
}

func TestSubmit_RAGAugmentation(t *testing.T) {
	ms := &mockStore{
		results: []rag.SearchResult{{Document: rag.Document{Content: "rag context"}}},
	}
	mp := &mockProvider{response: &model.Response{}}
	h, err := New(testConfig(), WithProvider(mp), WithStore(ms))
	if err != nil {
		t.Fatal(err)
	}

	_, _ = h.Submit(context.Background(), []filter.Message{{Role: "user", Content: "query"}})
	if mp.lastReq == nil || len(mp.lastReq.Messages) < 2 {
		t.Fatal("expected prepended system message")
	}
	if mp.lastReq.Messages[0].Role != "system" || !strings.Contains(mp.lastReq.Messages[0].Content, "rag context") {
		t.Errorf("expected system message with 'rag context'")
	}
}

type upperFilter struct{}

func (f *upperFilter) Name() string { return "upper" }
func (f *upperFilter) Process(ctx context.Context, msg *filter.Message) (*filter.Message, error) {
	return &filter.Message{Role: msg.Role, Content: strings.ToUpper(msg.Content), Metadata: msg.Metadata}, nil
}

func TestSubmit_InputFilterApplied(t *testing.T) {
	mp := &mockProvider{response: &model.Response{}}
	h, _ := New(testConfig(), WithProvider(mp))
	h.SetInputFilters([]filter.Filter{&upperFilter{}})
	_, _ = h.Submit(context.Background(), []filter.Message{{Role: "user", Content: "hello"}})
	if mp.lastReq.Messages[0].Content != "HELLO" {
		t.Errorf("expected uppercased message, got %s", mp.lastReq.Messages[0].Content)
	}
}

type suffixFilter struct{ suffix string }

func (f *suffixFilter) Name() string { return "suffix" }
func (f *suffixFilter) Process(ctx context.Context, msg *filter.Message) (*filter.Message, error) {
	return &filter.Message{Role: msg.Role, Content: msg.Content + f.suffix, Metadata: msg.Metadata}, nil
}

func TestSubmit_OutputFilterApplied(t *testing.T) {
	mp := &mockProvider{response: &model.Response{Content: "hello"}}
	h, _ := New(testConfig(), WithProvider(mp))
	h.SetOutputFilters([]filter.Filter{&suffixFilter{suffix: " world"}})
	resp, _ := h.Submit(context.Background(), []filter.Message{{Role: "user", Content: "hi"}})
	if resp.Content != "hello world" {
		t.Errorf("expected 'hello world', got %s", resp.Content)
	}
}

func TestClose(t *testing.T) {
	h, _ := New(testConfig(), WithProvider(&mockProvider{}))
	if err := h.Close(); err != nil {
		t.Errorf("unexpected error on close: %v", err)
	}
}

func TestBuildFilters(t *testing.T) {
	filters, err := BuildFilters([]config.FilterEntry{{Type: "PASSTHROUGH"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(filters) != 1 {
		t.Errorf("expected 1 filter")
	}

	_, err = BuildFilters([]config.FilterEntry{{Type: "UNKNOWN"}})
	if err == nil {
		t.Errorf("expected error for unknown type")
	}
}

func TestParseSurrealCreds(t *testing.T) {
	c := parseSurrealCreds("admin:pass")
	if c.user != "admin" || c.pass != "pass" {
		t.Errorf("failed to parse creds")
	}
	c = parseSurrealCreds("")
	if c.user != "root" || c.pass != "root" {
		t.Errorf("expected defaults")
	}
}
