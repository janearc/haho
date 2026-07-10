package health

import (
	"sync"
	"testing"
	"time"
)

func TestNewTracker(t *testing.T) {
	tr := NewTracker()
	status := tr.GetStatus()
	if status.State != StateIdle {
		t.Errorf("expected initial state StateIdle, got %v", status.State)
	}
}

func TestTracker_SetState(t *testing.T) {
	tr := NewTracker()
	tr.SetState(StateBusy, "processing request")
	status := tr.GetStatus()
	if status.State != StateBusy {
		t.Errorf("expected StateBusy, got %v", status.State)
	}
	if status.Message != "processing request" {
		t.Errorf("expected message 'processing request', got %q", status.Message)
	}
}

func TestTracker_SetModel(t *testing.T) {
	tr := NewTracker()
	tr.SetModel("llama3")
	status := tr.GetStatus()
	if status.Model != "llama3" {
		t.Errorf("expected model 'llama3', got %q", status.Model)
	}
}

func TestTracker_Uptime(t *testing.T) {
	tr := NewTracker()
	time.Sleep(10 * time.Millisecond)
	status := tr.GetStatus()
	if status.Uptime <= 0 {
		t.Errorf("expected uptime > 0, got %v", status.Uptime)
	}
}

func TestTracker_ConcurrentAccess(t *testing.T) {
	tr := NewTracker()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			tr.SetState(State(n%4), "concurrent")
			_ = tr.GetStatus()
		}(i)
	}
	wg.Wait()
	// If we reach here without a race detector complaint, the test passes.
	status := tr.GetStatus()
	if status == nil {
		t.Fatal("expected non-nil status")
	}
}

func TestState_String(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{StateIdle, "idle"},
		{StateBusy, "busy"},
		{StateWaitingSubprocess, "waiting_subprocess"},
		{StateWaitingInput, "waiting_input"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("State(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestState_String_Unknown(t *testing.T) {
	unknown := State(99)
	got := unknown.String()
	if got != "" {
		t.Errorf("expected empty string for unknown state, got %q", got)
	}
}
