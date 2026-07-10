package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SurrealStore implements Store using SurrealDB's HTTP API.
// SurrealDB is used for both RAG document storage and retrieval.
type SurrealStore struct {
	endpoint  string // e.g. http://surrealdb:8000
	namespace string
	database  string
	username  string
	password  string
	client    *http.Client
}

// surrealResponse represents a single response element from SurrealDB's /sql endpoint.
type surrealResponse struct {
	Result json.RawMessage `json:"result"`
	Status string          `json:"status"`
	Time   string          `json:"time"`
}

// NewSurrealStore creates a new SurrealStore connected to the given SurrealDB instance.
func NewSurrealStore(endpoint, namespace, database, username, password string) *SurrealStore {
	return &SurrealStore{
		endpoint:  strings.TrimRight(endpoint, "/"),
		namespace: namespace,
		database:  database,
		username:  username,
		password:  password,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Index stores documents in SurrealDB. Builds a multi-statement SurrealQL query
// for batch efficiency.
func (s *SurrealStore) Index(ctx context.Context, docs []Document) error {
	if len(docs) == 0 {
		return nil
	}

	var stmts []string
	for _, doc := range docs {
		meta, err := json.Marshal(doc.Metadata)
		if err != nil {
			return fmt.Errorf("marshaling metadata for doc %s: %w", doc.ID, err)
		}
		emb, err := json.Marshal(doc.Embedding)
		if err != nil {
			return fmt.Errorf("marshaling embedding for doc %s: %w", doc.ID, err)
		}
		// Escape single quotes in content for SurrealQL.
		content := strings.ReplaceAll(doc.Content, "'", "\\'")
		stmt := fmt.Sprintf(
			"CREATE documents:%s SET content = '%s', embedding = %s, metadata = %s",
			doc.ID, content, string(emb), string(meta),
		)
		stmts = append(stmts, stmt)
	}

	query := strings.Join(stmts, ";\n") + ";"
	_, err := s.doQuery(ctx, query)
	return err
}

// Search performs a full-text search against documents in SurrealDB.
func (s *SurrealStore) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	escapedQuery := strings.ReplaceAll(query, "'", "\\'")
	surrealQL := fmt.Sprintf(
		"SELECT *, search::score(1) AS score FROM documents WHERE content @1@ '%s' ORDER BY score DESC LIMIT %d",
		escapedQuery, limit,
	)

	raw, err := s.doQuery(ctx, surrealQL)
	if err != nil {
		return nil, err
	}

	// Parse the SurrealDB response array.
	var responses []surrealResponse
	if err := json.Unmarshal(raw, &responses); err != nil {
		return nil, fmt.Errorf("parsing surreal response: %w", err)
	}

	if len(responses) == 0 {
		return nil, nil
	}

	if responses[0].Status != "OK" {
		return nil, fmt.Errorf("surreal query failed: %s", string(responses[0].Result))
	}

	// Parse result rows into search results.
	type resultRow struct {
		ID       string            `json:"id"`
		Content  string            `json:"content"`
		Metadata map[string]string `json:"metadata"`
		Score    float64           `json:"score"`
	}

	var rows []resultRow
	if err := json.Unmarshal(responses[0].Result, &rows); err != nil {
		return nil, fmt.Errorf("parsing result rows: %w", err)
	}

	results := make([]SearchResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, SearchResult{
			Document: Document{
				ID:       row.ID,
				Content:  row.Content,
				Metadata: row.Metadata,
			},
			Score: row.Score,
		})
	}

	return results, nil
}

// Close is a no-op for SurrealStore (HTTP client needs no teardown).
func (s *SurrealStore) Close() error { return nil }

// doQuery executes a SurrealQL query via the SurrealDB HTTP API.
func (s *SurrealStore) doQuery(ctx context.Context, query string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint+"/sql", bytes.NewBufferString(query))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("NS", s.namespace)
	req.Header.Set("DB", s.database)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(s.username, s.password)

	resp, err := s.client.Do(req)
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
