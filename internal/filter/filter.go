package filter

import "context"

// Message represents a chat message flowing through the filter pipeline.
type Message struct {
	Role     string            `json:"role"`
	Content  string            `json:"content"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Filter processes a message, potentially transforming it.
// Implementations must be safe for concurrent use.
type Filter interface {
	// Name returns the filter's identifier.
	Name() string
	// Process transforms a message. Returns the (possibly modified) message or an error.
	Process(ctx context.Context, msg *Message) (*Message, error)
}
