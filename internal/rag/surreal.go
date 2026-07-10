package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// SurrealStore implements Store using SurrealDB's HTTP /sql API (SurrealDB 2.x).
// All user-supplied values are bound as query-string variables ($name) rather
// than interpolated into the query text, so message content and document IDs
// cannot inject SurrealQL.
type SurrealStore struct {
	endpoint  string // e.g. http://surrealdb:8000
	namespace string
	database  string
	username  string
	password  string
	client    *http.Client
}

// analyzerName and searchIndexName are the schema objects EnsureSchema creates.
// The search index is reference 1, which the Search query addresses via the
// @1@ operator and search::score(1).
const (
	analyzerName    = "haho_analyzer"
	searchIndexName = "haho_doc_content"
)

// surrealResponse is one element of SurrealDB's /sql response array (one per
// statement in the submitted query).
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

// EnsureSchema creates the full-text analyzer and search index the Search query
// depends on. It is idempotent (IF NOT EXISTS) and safe to call on every start.
func (s *SurrealStore) EnsureSchema(ctx context.Context) error {
	query := fmt.Sprintf(
		"DEFINE ANALYZER IF NOT EXISTS %s TOKENIZERS blank,class FILTERS lowercase,ascii; "+
			"DEFINE INDEX IF NOT EXISTS %s ON documents FIELDS content SEARCH ANALYZER %s BM25 HIGHLIGHTS;",
		analyzerName, searchIndexName, analyzerName,
	)
	responses, err := s.doQuery(ctx, query, nil)
	if err != nil {
		return err
	}
	return firstStatementError(responses)
}

// Index stores documents in SurrealDB. Values are bound as $idN/$cN/$eN/$mN
// query variables; nothing user-supplied reaches the query text.
func (s *SurrealStore) Index(ctx context.Context, docs []Document) error {
	if len(docs) == 0 {
		return nil
	}

	var stmts []string
	vars := make(map[string]string, len(docs)*4)
	for i, doc := range docs {
		meta, err := json.Marshal(doc.Metadata)
		if err != nil {
			return fmt.Errorf("marshaling metadata for doc %s: %w", doc.ID, err)
		}
		emb, err := json.Marshal(doc.Embedding)
		if err != nil {
			return fmt.Errorf("marshaling embedding for doc %s: %w", doc.ID, err)
		}
		stmts = append(stmts, fmt.Sprintf(
			"CREATE type::thing(\"documents\", <string>$id%d) SET content = <string>$c%d, embedding = $e%d, metadata = $m%d",
			i, i, i, i,
		))
		vars[fmt.Sprintf("id%d", i)] = doc.ID
		vars[fmt.Sprintf("c%d", i)] = doc.Content
		vars[fmt.Sprintf("e%d", i)] = string(emb)
		vars[fmt.Sprintf("m%d", i)] = string(meta)
	}

	query := strings.Join(stmts, ";\n") + ";"
	responses, err := s.doQuery(ctx, query, vars)
	if err != nil {
		return err
	}
	return firstStatementError(responses)
}

// Search performs a BM25 full-text search over indexed documents.
func (s *SurrealStore) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 3
	}
	// $query is bound (not interpolated), so it cannot inject SurrealQL. It is
	// intentionally not <string>-cast: a cast wraps the value in an expression
	// that defeats the full-text index matcher ("no suitable index").
	surrealQL := "SELECT meta::id(id) AS id, content, metadata, search::score(1) AS score " +
		"FROM documents WHERE content @1@ $query ORDER BY score DESC LIMIT $limit"

	responses, err := s.doQuery(ctx, surrealQL, map[string]string{
		"query": query,
		"limit": strconv.Itoa(limit),
	})
	if err != nil {
		return nil, err
	}
	if len(responses) == 0 {
		return nil, nil
	}
	if err := firstStatementError(responses); err != nil {
		return nil, err
	}

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

// doQuery executes a SurrealQL query via the SurrealDB HTTP /sql API. The query
// text goes in the request body; vars are bound as $name query-string
// parameters (SurrealDB 2.x reads URL query params as query variables).
func (s *SurrealStore) doQuery(ctx context.Context, query string, vars map[string]string) ([]surrealResponse, error) {
	return doSurrealQuery(ctx, s.client, s.endpoint, s.namespace, s.database, s.username, s.password, query, vars)
}

// doSurrealQuery is the shared HTTP path for the store and the cache.
func doSurrealQuery(ctx context.Context, client *http.Client, endpoint, namespace, database, username, password, query string, vars map[string]string) ([]surrealResponse, error) {
	target := endpoint + "/sql"
	if len(vars) > 0 {
		q := url.Values{}
		for k, v := range vars {
			q.Set(k, v)
		}
		target += "?" + q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(query))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("surreal-ns", namespace)
	req.Header.Set("surreal-db", database)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "text/plain")
	req.SetBasicAuth(username, password)

	resp, err := client.Do(req)
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

	var responses []surrealResponse
	if err := json.Unmarshal(body, &responses); err != nil {
		return nil, fmt.Errorf("parsing surreal response: %w", err)
	}
	return responses, nil
}

// firstStatementError returns an error if any statement in the response failed.
func firstStatementError(responses []surrealResponse) error {
	for _, r := range responses {
		if r.Status != "OK" {
			return fmt.Errorf("surreal statement failed (%s): %s", r.Status, string(r.Result))
		}
	}
	return nil
}
