package rag

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/janearc/haho/internal/model"
)

// Cache provides response caching backed by SurrealDB. Entries carry a real
// expiry (expires_at), and Get filters on it, so a cached response is returned
// only while it is still live.
type Cache interface {
	Get(ctx context.Context, key string) (*model.Response, bool, error)
	Set(ctx context.Context, key string, resp *model.Response, ttl time.Duration) error
}

// SurrealCache implements Cache using SurrealDB's HTTP /sql API (2.x).
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

// CacheKey generates a deterministic SHA-256 cache key from the messages + model.
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

// Get retrieves a cached response by key. Returns (nil, false, nil) on cache
// miss or expiry.
func (c *SurrealCache) Get(ctx context.Context, key string) (*model.Response, bool, error) {
	query := "SELECT response FROM type::thing(\"cache\", <string>$key) WHERE expires_at > time::now()"
	responses, err := c.doQuery(ctx, query, map[string]string{"key": key})
	if err != nil {
		return nil, false, fmt.Errorf("cache get: %w", err)
	}
	if len(responses) == 0 || responses[0].Status != "OK" {
		return nil, false, nil
	}

	type cacheRow struct {
		Response json.RawMessage `json:"response"`
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

// Set stores a response in the cache with the given TTL. A non-positive TTL
// yields an already-expired entry (i.e. an effective no-op), so callers should
// pass the configured TTL.
func (c *SurrealCache) Set(ctx context.Context, key string, resp *model.Response, ttl time.Duration) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshaling response: %w", err)
	}

	// SurrealDB parses the object-shaped $resp value into a record object;
	// $ttl (e.g. "3600s") is cast to a duration. The unit suffix is required
	// so $ttl is not inferred as a bare number.
	ttlStr := fmt.Sprintf("%ds", int(ttl.Seconds()))
	query := "UPSERT type::thing(\"cache\", <string>$key) SET response = $resp, expires_at = time::now() + type::duration($ttl)"

	responses, err := c.doQuery(ctx, query, map[string]string{
		"key":  key,
		"resp": string(data),
		"ttl":  ttlStr,
	})
	if err != nil {
		return fmt.Errorf("cache set: %w", err)
	}
	return firstStatementError(responses)
}

// doQuery executes a SurrealQL query via the shared SurrealDB HTTP path.
func (c *SurrealCache) doQuery(ctx context.Context, query string, vars map[string]string) ([]surrealResponse, error) {
	return doSurrealQuery(ctx, c.client, c.endpoint, c.namespace, c.database, c.username, c.password, query, vars)
}
