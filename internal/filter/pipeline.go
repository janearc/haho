package filter

import (
	"context"
	"fmt"
	"sync"
)

// Pipeline chains multiple filters in order.
type Pipeline struct {
	mu      sync.RWMutex
	filters []Filter
}

// NewPipeline creates a pipeline with the given filters applied in order.
func NewPipeline(filters ...Filter) *Pipeline {
	f := make([]Filter, len(filters))
	copy(f, filters)
	return &Pipeline{
		filters: f,
	}
}

// Process runs the message through each filter in sequence.
// If any filter returns an error, processing stops and the error is returned.
func (p *Pipeline) Process(ctx context.Context, msg *Message) (*Message, error) {
	p.mu.RLock()
	filters := make([]Filter, len(p.filters))
	copy(filters, p.filters)
	p.mu.RUnlock()

	// Clone the message before processing (don't mutate the input).
	current := &Message{
		Role:    msg.Role,
		Content: msg.Content,
	}
	if msg.Metadata != nil {
		current.Metadata = make(map[string]string, len(msg.Metadata))
		for k, v := range msg.Metadata {
			current.Metadata[k] = v
		}
	}

	for _, f := range filters {
		var err error
		current, err = f.Process(ctx, current)
		if err != nil {
			return nil, fmt.Errorf("filter %q: %w", f.Name(), err)
		}
	}

	return current, nil
}

// SetFilters replaces the pipeline's filters atomically.
func (p *Pipeline) SetFilters(filters []Filter) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.filters = make([]Filter, len(filters))
	copy(p.filters, filters)
}

// Filters returns a copy of the current filter list.
func (p *Pipeline) Filters() []Filter {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]Filter, len(p.filters))
	copy(out, p.filters)
	return out
}
