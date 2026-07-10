package filter

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// errFilter is a test filter that always returns a configured error.
type errFilter struct {
	err error
}

func (f *errFilter) Name() string { return "error" }
func (f *errFilter) Process(ctx context.Context, msg *Message) (*Message, error) {
	return nil, f.err
}

// ctxFilter is a test filter that checks context cancellation.
type ctxFilter struct{}

func (f *ctxFilter) Name() string { return "ctx" }
func (f *ctxFilter) Process(ctx context.Context, msg *Message) (*Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return msg, nil
}

func TestPassthroughFilter(t *testing.T) {
	f := NewPassthroughFilter()
	msg := &Message{Role: "user", Content: "hello"}
	out, err := f.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Content != "hello" {
		t.Errorf("content = %q, want %q", out.Content, "hello")
	}
	if out.Role != "user" {
		t.Errorf("role = %q, want %q", out.Role, "user")
	}
	if out == msg {
		t.Error("expected a copy, got same pointer")
	}
}

func TestPassthroughFilter_PreservesMetadata(t *testing.T) {
	f := NewPassthroughFilter()
	msg := &Message{
		Role:     "assistant",
		Content:  "response",
		Metadata: map[string]string{"key": "value", "foo": "bar"},
	}
	out, err := f.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Metadata["key"] != "value" {
		t.Errorf("metadata[key] = %q, want %q", out.Metadata["key"], "value")
	}
	if out.Metadata["foo"] != "bar" {
		t.Errorf("metadata[foo] = %q, want %q", out.Metadata["foo"], "bar")
	}
}

func TestLoggerFilter(t *testing.T) {
	var buf bytes.Buffer
	f := NewLoggerFilter(&buf)
	msg := &Message{Role: "user", Content: "test message"}
	out, err := f.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "test message") {
		t.Errorf("log output %q does not contain %q", buf.String(), "test message")
	}
	if !strings.Contains(buf.String(), "role=user") {
		t.Errorf("log output %q does not contain role", buf.String())
	}
	if out.Content != "test message" {
		t.Errorf("content = %q, want %q", out.Content, "test message")
	}
}

func TestLoggerFilter_TruncatesLongContent(t *testing.T) {
	var buf bytes.Buffer
	f := NewLoggerFilter(&buf)
	longContent := strings.Repeat("a", 500)
	msg := &Message{Role: "user", Content: longContent}
	_, err := f.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	logged := buf.String()
	if !strings.Contains(logged, "...") {
		t.Error("expected truncation marker '...' in log output")
	}
	// The logged content should not contain all 500 chars.
	if strings.Contains(logged, longContent) {
		t.Error("expected log to truncate content, but found full content")
	}
}

func TestLoggerFilter_NilWriter(t *testing.T) {
	f := NewLoggerFilter(nil)
	msg := &Message{Role: "user", Content: "nil writer test"}
	out, err := f.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Content != "nil writer test" {
		t.Errorf("content = %q, want %q", out.Content, "nil writer test")
	}
}

func TestPipeline_Empty(t *testing.T) {
	p := NewPipeline()
	msg := &Message{Role: "user", Content: "hello"}
	out, err := p.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Content != "hello" {
		t.Errorf("content = %q, want %q", out.Content, "hello")
	}
	if out == msg {
		t.Error("expected a copy, got same pointer")
	}
}

func TestPipeline_SingleFilter(t *testing.T) {
	p := NewPipeline(NewPassthroughFilter())
	msg := &Message{Role: "user", Content: "hello"}
	out, err := p.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Content != "hello" {
		t.Errorf("content = %q, want %q", out.Content, "hello")
	}
}

func TestPipeline_MultipleFilters(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLoggerFilter(&buf)
	passthrough := NewPassthroughFilter()
	p := NewPipeline(logger, passthrough)

	msg := &Message{Role: "user", Content: "pipeline test"}
	out, err := p.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "pipeline test") {
		t.Errorf("logger did not capture content: %q", buf.String())
	}
	if out.Content != "pipeline test" {
		t.Errorf("content = %q, want %q", out.Content, "pipeline test")
	}
}

func TestPipeline_SetFilters(t *testing.T) {
	p := NewPipeline()
	if len(p.Filters()) != 0 {
		t.Fatalf("expected 0 filters, got %d", len(p.Filters()))
	}

	newFilters := []Filter{NewPassthroughFilter(), NewPassthroughFilter()}
	p.SetFilters(newFilters)

	got := p.Filters()
	if len(got) != 2 {
		t.Fatalf("expected 2 filters, got %d", len(got))
	}
	for i, f := range got {
		if f.Name() != "passthrough" {
			t.Errorf("filter[%d].Name() = %q, want %q", i, f.Name(), "passthrough")
		}
	}
}

func TestPipeline_ErrorStopsChain(t *testing.T) {
	expectedErr := errors.New("test error")
	p := NewPipeline(
		NewPassthroughFilter(),
		&errFilter{err: expectedErr},
		NewPassthroughFilter(),
	)

	msg := &Message{Role: "user", Content: "hello"}
	_, err := p.Process(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("error = %v, want wrapping %v", err, expectedErr)
	}
}

func TestPipeline_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	p := NewPipeline(&ctxFilter{})
	msg := &Message{Role: "user", Content: "hello"}
	_, err := p.Process(ctx, msg)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

func TestRTKFilter_BinaryNotFound(t *testing.T) {
	_, err := NewRTKFilter("/nonexistent/path/to/rtk")
	// NewRTKFilter with a non-empty path does not validate existence;
	// the error would come from Process. So we test LookPath failure instead.
	// Actually, per spec, if binaryPath is provided we just use it.
	// Test the LookPath path with a guaranteed-missing binary.
	_, err = NewRTKFilter("", /* no args */)
	if err == nil {
		// rtk might actually be on PATH; only fail if we expected an error
		if _, lookErr := exec.LookPath("rtk"); lookErr != nil {
			t.Fatal("expected error for missing rtk binary, got nil")
		}
	}
}

func TestRTKFilter_LookPath(t *testing.T) {
	if _, err := exec.LookPath("rtk"); err != nil {
		t.Skip("rtk not on PATH, skipping")
	}
	f, err := NewRTKFilter("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Name() != "rtk" {
		t.Errorf("Name() = %q, want %q", f.Name(), "rtk")
	}
}
