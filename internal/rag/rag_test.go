package rag

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/janearc/haho/internal/model"
)

// mockSurrealResponse builds a SurrealDB-formatted JSON response.
func mockSurrealResponse(result interface{}) []byte {
	resultJSON, _ := json.Marshal(result)
	resp := []map[string]interface{}{
		{
			"result": json.RawMessage(resultJSON),
			"status": "OK",
			"time":   "1ms",
		},
	}
	data, _ := json.Marshal(resp)
	return data
}

func TestCacheKey_Deterministic(t *testing.T) {
	msgs := []string{"hello", "world"}
	k1 := CacheKey(msgs, "gpt-4")
	k2 := CacheKey(msgs, "gpt-4")
	if k1 != k2 {
		t.Errorf("expected deterministic keys, got %q and %q", k1, k2)
	}
}

func TestCacheKey_DifferentInputs(t *testing.T) {
	k1 := CacheKey([]string{"hello"}, "gpt-4")
	k2 := CacheKey([]string{"goodbye"}, "gpt-4")
	if k1 == k2 {
		t.Error("expected different keys for different inputs")
	}

	k3 := CacheKey([]string{"hello"}, "gpt-4")
	k4 := CacheKey([]string{"hello"}, "llama3")
	if k3 == k4 {
		t.Error("expected different keys for different models")
	}
}

func TestSurrealStore_Index(t *testing.T) {
	var capturedNS, capturedDB string
	var capturedBody string
	var capturedQuery url.Values

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sql" {
			t.Errorf("expected /sql path, got %s", r.URL.Path)
		}
		capturedNS = r.Header.Get("surreal-ns")
		capturedDB = r.Header.Get("surreal-db")
		capturedQuery = r.URL.Query()

		bodyBytes, _ := io.ReadAll(r.Body)
		capturedBody = string(bodyBytes)

		w.Header().Set("Content-Type", "application/json")
		w.Write(mockSurrealResponse([]interface{}{}))
	}))
	defer server.Close()

	store := NewSurrealStore(server.URL, "test_ns", "test_db", "user", "pass")
	docs := []Document{
		{ID: "doc1", Content: "hello world", Metadata: map[string]string{"source": "test"}},
		{ID: "doc2", Content: "foo bar"},
	}

	err := store.Index(context.Background(), docs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedNS != "test_ns" {
		t.Errorf("expected surreal-ns header 'test_ns', got %q", capturedNS)
	}
	if capturedDB != "test_db" {
		t.Errorf("expected surreal-db header 'test_db', got %q", capturedDB)
	}
	// IDs and content are bound as query-string variables, never interpolated
	// into the query body (this is the injection-safety property).
	if capturedQuery.Get("id0") != "doc1" || capturedQuery.Get("id1") != "doc2" {
		t.Errorf("expected doc IDs bound as $idN query vars, got %v", capturedQuery)
	}
	if capturedQuery.Get("c0") != "hello world" {
		t.Errorf("expected content bound as $c0 query var, got %q", capturedQuery.Get("c0"))
	}
	if strings.Contains(capturedBody, "doc1") || strings.Contains(capturedBody, "hello world") {
		t.Errorf("doc IDs/content must not appear in the query body, got %q", capturedBody)
	}
	if !strings.Contains(capturedBody, "CREATE") {
		t.Errorf("expected CREATE statements in body, got %q", capturedBody)
	}
}

func TestSurrealStore_Search(t *testing.T) {
	results := []map[string]interface{}{
		{"id": "documents:doc1", "content": "hello world", "score": 0.95, "metadata": map[string]string{"source": "test"}},
		{"id": "documents:doc2", "content": "hello there", "score": 0.80},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(mockSurrealResponse(results))
	}))
	defer server.Close()

	store := NewSurrealStore(server.URL, "ns", "db", "user", "pass")
	searchResults, err := store.Search(context.Background(), "hello", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(searchResults) != 2 {
		t.Fatalf("expected 2 results, got %d", len(searchResults))
	}

	if searchResults[0].Score != 0.95 {
		t.Errorf("expected score 0.95, got %f", searchResults[0].Score)
	}
	if searchResults[0].Document.Content != "hello world" {
		t.Errorf("expected content 'hello world', got %q", searchResults[0].Document.Content)
	}
}

func TestSurrealStore_Search_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(mockSurrealResponse([]interface{}{}))
	}))
	defer server.Close()

	store := NewSurrealStore(server.URL, "ns", "db", "user", "pass")
	results, err := store.Search(context.Background(), "nothing", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestSurrealCache_Get_Hit(t *testing.T) {
	cachedResp := model.Response{
		Content:    "cached answer",
		TokensUsed: 42,
		Model:      "gpt-4",
		Cached:     true,
	}
	respJSON, _ := json.Marshal(cachedResp)

	rows := []map[string]interface{}{
		{"response": json.RawMessage(respJSON)},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(mockSurrealResponse(rows))
	}))
	defer server.Close()

	cache := NewSurrealCache(server.URL, "ns", "db", "user", "pass")
	resp, hit, err := cache.Get(context.Background(), "testkey")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hit {
		t.Fatal("expected cache hit")
	}
	if resp.Content != "cached answer" {
		t.Errorf("expected 'cached answer', got %q", resp.Content)
	}
	if resp.TokensUsed != 42 {
		t.Errorf("expected 42 tokens, got %d", resp.TokensUsed)
	}
}

func TestSurrealCache_Get_Miss(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return empty result set.
		w.Write(mockSurrealResponse([]interface{}{}))
	}))
	defer server.Close()

	cache := NewSurrealCache(server.URL, "ns", "db", "user", "pass")
	resp, hit, err := cache.Get(context.Background(), "missingkey")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hit {
		t.Error("expected cache miss")
	}
	if resp != nil {
		t.Errorf("expected nil response on miss, got %v", resp)
	}
}

func TestSurrealCache_Set(t *testing.T) {
	var capturedBody string
	var capturedQuery url.Values

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sql" {
			t.Errorf("expected /sql path, got %s", r.URL.Path)
		}
		capturedQuery = r.URL.Query()
		bodyBytes, _ := io.ReadAll(r.Body)
		capturedBody = string(bodyBytes)

		w.Header().Set("Content-Type", "application/json")
		w.Write(mockSurrealResponse([]interface{}{}))
	}))
	defer server.Close()

	cache := NewSurrealCache(server.URL, "ns", "db", "user", "pass")
	resp := &model.Response{
		Content:    "test response",
		TokensUsed: 10,
		Model:      "llama3",
	}

	err := cache.Set(context.Background(), "setkey", resp, 5*time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(capturedBody, "UPSERT") {
		t.Errorf("expected UPSERT in body, got %q", capturedBody)
	}
	// The response payload and TTL are bound as query-string variables.
	if !strings.Contains(capturedQuery.Get("resp"), "test response") {
		t.Errorf("expected response content bound as $resp query var, got %q", capturedQuery.Get("resp"))
	}
	if capturedQuery.Get("ttl") != "300s" {
		t.Errorf("expected TTL '300s' bound as $ttl query var, got %q", capturedQuery.Get("ttl"))
	}
	if strings.Contains(capturedBody, "test response") {
		t.Errorf("response content must not appear in the query body, got %q", capturedBody)
	}
}

func TestSurrealCache_Get_Expired(t *testing.T) {
	// Server-side expiry: the SurrealQL query filters by expires_at > time::now().
	// For expired entries, the server returns an empty result set.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(mockSurrealResponse([]interface{}{}))
	}))
	defer server.Close()

	cache := NewSurrealCache(server.URL, "ns", "db", "user", "pass")
	resp, hit, err := cache.Get(context.Background(), "expiredkey")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hit {
		t.Error("expected cache miss for expired entry")
	}
	if resp != nil {
		t.Errorf("expected nil response for expired entry, got %v", resp)
	}
}
