package filter

import "context"

// PassthroughFilter passes messages through unmodified.
type PassthroughFilter struct{}

// NewPassthroughFilter creates a new PassthroughFilter.
func NewPassthroughFilter() *PassthroughFilter { return &PassthroughFilter{} }

// Name returns the filter's identifier.
func (f *PassthroughFilter) Name() string { return "passthrough" }

// Process returns a copy of the message (don't return the same pointer).
func (f *PassthroughFilter) Process(ctx context.Context, msg *Message) (*Message, error) {
	out := &Message{
		Role:    msg.Role,
		Content: msg.Content,
	}
	if msg.Metadata != nil {
		out.Metadata = make(map[string]string, len(msg.Metadata))
		for k, v := range msg.Metadata {
			out.Metadata[k] = v
		}
	}
	return out, nil
}
