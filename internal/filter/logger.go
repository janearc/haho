package filter

import (
	"context"
	"io"
	"log"
	"os"
)

// LoggerFilter logs message content to a writer, then passes through.
// Designed as an output filter to capture all responses.
type LoggerFilter struct {
	logger *log.Logger
	writer io.Writer
}

// NewLoggerFilter creates a logger filter. If w is nil, logs to stderr.
func NewLoggerFilter(w io.Writer) *LoggerFilter {
	if w == nil {
		w = os.Stderr
	}
	return &LoggerFilter{
		logger: log.New(w, "[haho:filter:logger] ", log.LstdFlags),
		writer: w,
	}
}

// Name returns the filter's identifier.
func (f *LoggerFilter) Name() string { return "logger" }

// Process logs the message content and passes through.
func (f *LoggerFilter) Process(ctx context.Context, msg *Message) (*Message, error) {
	content := msg.Content
	if len(content) > 200 {
		content = content[:200] + "..."
	}
	f.logger.Printf("role=%s content=%s", msg.Role, content)

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

// Writer returns the underlying writer for the logger filter.
func (f *LoggerFilter) Writer() io.Writer {
	return f.writer
}
