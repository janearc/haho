package rag

import "context"

// Document represents a document stored for RAG retrieval.
type Document struct {
	ID        string            `json:"id"`
	Content   string            `json:"content"`
	Embedding []float64         `json:"embedding,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// SearchResult represents a RAG search result.
type SearchResult struct {
	Document Document `json:"document"`
	Score    float64  `json:"score"`
}

// Store provides RAG document storage and retrieval.
type Store interface {
	Index(ctx context.Context, docs []Document) error
	Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
	Close() error
}
