package rag

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/janearc/haho/internal/model"
)

// Cache provides response caching backed by SurrealDB.
// This avoids costly cache lookups across the wire by using SurrealDB
// (which is already in the k3s cluster) as the cache store.
type Cache interface {
	Get(ctx context.Context, key string) (*model.Response, bool, error)
	Set(ctx context.Context, key string, resp *model.Response, ttl time.Duration) error
}

// SurrealCache implements Cache using SurrealDB.
type SurrealCache struct {
	endpoint  string
	namespace string
	database  string
	username  string
	password  string
	client    *http.Client
}

// NewSurrealCache creates a new SurrealCache connected to the given SurrealDB instance.
func NewSurrealCache(endpoint, namespace, database, username, password string) *SurrealCache {
	return &SurrealCache{
		endpoint:  strings.TrimRight(endpoint, "/"),
		namespace: namespace,
		database:  database,
		username:  username,
		password:  password,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// cacheKeyInput is the struct serialized for deterministic cache key generation.
type cacheKeyInput struct {
	Messages interface{} `json:"messages"`
	Model    string      `json:"model"`
}

// CacheKey generates a deterministic cache key from a request.
// Uses SHA-256 hash of the serialized messages + model + params.
func CacheKey(messages interface{}, modelName string) string {
	input := cacheKeyInput{
		Messages: messages,
		Model:    modelName,
	}
	data, err := json.Marshal(input)
	if err != nil {
		// Fallback: hash the model name alone. This shouldn't happen in practice.
		data = []byte(modelName)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// surrealCacheResponse represents a single response element from SurrealDB.
type surrealCacheResponse struct {
	Result json.RawMessage `json:"result"`
	Status string          `json:"status"`
}

// cacheRow represents a row stored in the cache table.
type cacheRow struct {
	Response json.RawMessage `json:"response"`
}

// Get retrieves a cached response by key. Returns (nil, false, nil) on cache miss.
func (c *SurrealCache) Get(ctx context.Context, key string) (*model.Response, bool, error) {
	query := fmt.Sprintf("SELECT response FROM cache WHERE id = cache:%s AND expires_at > time::now()", key)

	raw, err := c.doQuery(ctx, query)
	if err != nil {
		return nil, false, fmt.Errorf("cache get: %w", err)
	}

	var responses []surrealCacheResponse
	if err := json.Unmarshal(raw, &responses); err != nil {
		return nil, false, fmt.Errorf("parsing cache response: %w", err)
	}

	if len(responses) == 0 || responses[0].Status != "OK" {
		return nil, false, nil
	}

	var rows []cacheRow
	if err := json.Unmarshal(responses[0].Result, &rows); err != nil {
		return nil, false, fmt.Errorf("parsing cache rows: %w", err)
	}

	if len(rows) == 0 {
		return nil, false, nil
	}

	var resp model.Response
	if err := json.Unmarshal(rows[0].Response, &resp); err != nil {
		return nil, false, fmt.Errorf("unmarshaling cached response: %w", err)
	}

	return &resp, true, nil
}

// Set stores a response in the cache with the given TTL.
func (c *SurrealCache) Set(ctx context.Context, key string, resp *model.Response, ttl time.Duration) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshaling response: %w", err)
	}

	// Express TTL as SurrealDB duration string (e.g. "300s").
	ttlStr := fmt.Sprintf("%ds", int(ttl.Seconds()))

	query := fmt.Sprintf(
		"UPSERT cache:%s SET response = %s, expires_at = time::now() + %s",
		key, string(data), ttlStr,
	)

	_, err = c.doQuery(ctx, query)
	if err != nil {
		return fmt.Errorf("cache set: %w", err)
	}

	return nil
}

// doQuery executes a SurrealQL query via the SurrealDB HTTP API.
func (c *SurrealCache) doQuery(ctx context.Context, query string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/sql", bytes.NewBufferString(query))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("NS", c.namespace)
	req.Header.Set("DB", c.database)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.username, c.password)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing query: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("surreal HTTP %d: %s", resp.StatusCode, string(body))
	}

	return json.RawMessage(body), nil
}
