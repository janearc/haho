//go:build integration

package rag

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/janearc/haho/internal/model"
)

// These tests exercise the SurrealDB store and cache against a live SurrealDB
// 2.x instance. Point them at one with:
//
//	HAHO_TEST_SURREAL_URL=http://127.0.0.1:18001 \
//	HAHO_TEST_SURREAL_CREDS=root:root \
//	go test ./internal/rag/... -tags=integration -run Integration -v
func surrealTarget(t *testing.T) (endpoint, user, pass string) {
	t.Helper()
	endpoint = os.Getenv("HAHO_TEST_SURREAL_URL")
	if endpoint == "" {
		t.Skip("HAHO_TEST_SURREAL_URL not set; skipping live SurrealDB test")
	}
	user, pass = "root", "root"
	if creds := os.Getenv("HAHO_TEST_SURREAL_CREDS"); creds != "" {
		if i := strings.IndexByte(creds, ':'); i >= 0 {
			user, pass = creds[:i], creds[i+1:]
		}
	}
	return endpoint, user, pass
}

func TestIntegration_StoreIndexSearch(t *testing.T) {
	endpoint, user, pass := surrealTarget(t)
	ctx := context.Background()
	store := NewSurrealStore(endpoint, "haho_it", "haho_it", user, pass)

	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	// Start clean so the test is repeatable against a persistent DB.
	if _, err := store.doQuery(ctx, "DELETE documents;", nil); err != nil {
		t.Fatalf("clearing documents: %v", err)
	}

	docs := []Document{
		{ID: "itdoc1", Content: "the quick brown fox jumps over the lazy dog", Metadata: map[string]string{"source": "test"}},
		{ID: "itdoc2", Content: "a completely unrelated sentence about databases"},
		// A hostile payload: naive single-quote escaping would break out here.
		{ID: "itdoc3", Content: `injection attempt ' ); DROP TABLE documents; -- \ "quoted"`},
	}
	if err := store.Index(ctx, docs); err != nil {
		t.Fatalf("Index: %v", err)
	}

	results, err := store.Search(ctx, "quick fox", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one search result for 'quick fox'")
	}
	if !strings.Contains(results[0].Document.Content, "quick brown fox") {
		t.Errorf("expected the fox document ranked first, got %q", results[0].Document.Content)
	}

	// The injection payload must have been stored verbatim as data, and the
	// documents table must still exist (the DROP did not execute).
	all, err := store.Search(ctx, "injection attempt", 5)
	if err != nil {
		t.Fatalf("Search after injection payload: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("expected the injection-payload document to be searchable (table intact)")
	}
}

func TestIntegration_CacheRoundTrip(t *testing.T) {
	endpoint, user, pass := surrealTarget(t)
	ctx := context.Background()
	cache := NewSurrealCache(endpoint, "haho_it", "haho_it", user, pass)

	resp := &model.Response{
		Content:    `answer with "quotes", a brace {x}, and unicode café`,
		TokensUsed: 42,
		Model:      "test-model",
	}

	if err := cache.Set(ctx, "itkey", resp, time.Hour); err != nil {
		t.Fatalf("cache Set: %v", err)
	}
	got, hit, err := cache.Get(ctx, "itkey")
	if err != nil {
		t.Fatalf("cache Get: %v", err)
	}
	if !hit {
		t.Fatal("expected cache hit")
	}
	if got.Content != resp.Content || got.TokensUsed != resp.TokensUsed {
		t.Errorf("round-trip mismatch: got %+v want %+v", got, resp)
	}

	// A zero TTL yields an already-expired entry, so Get must miss.
	if err := cache.Set(ctx, "itexpired", resp, 0); err != nil {
		t.Fatalf("cache Set (expired): %v", err)
	}
	if _, hit, _ := cache.Get(ctx, "itexpired"); hit {
		t.Error("expected miss for a zero-TTL (expired) entry")
	}
}
