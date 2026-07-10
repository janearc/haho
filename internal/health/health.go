package health

import (
	"sync"
	"time"
)

// State represents the harness operational state.
type State int

const (
	StateIdle             State = iota // Ready for work
	StateBusy                          // Processing a request
	StateWaitingSubprocess             // Waiting on a child process (e.g. local model)
	StateWaitingInput                  // Blocked on user/upstream input
)

var stateNames = map[State]string{
	StateIdle:              "idle",
	StateBusy:              "busy",
	StateWaitingSubprocess: "waiting_subprocess",
	StateWaitingInput:      "waiting_input",
}

func (s State) String() string { return stateNames[s] }

// Status is a point-in-time snapshot of harness health.
type Status struct {
	State       State         `json:"state"`
	StateStr    string        `json:"state_str"`
	Model       string        `json:"model"`
	Uptime      time.Duration `json:"uptime"`
	LastUpdated time.Time     `json:"last_updated"`
	Message     string        `json:"message,omitempty"`
}

// Tracker tracks harness state. Thread-safe.
type Tracker interface {
	SetState(state State, msg string)
	SetModel(model string)
	GetStatus() *Status
}

// DefaultTracker is the standard Tracker implementation.
type DefaultTracker struct {
	mu         sync.RWMutex
	state      State
	model      string
	message    string
	startTime  time.Time
	lastUpdate time.Time
}

// NewTracker creates a new DefaultTracker with initial state StateIdle.
func NewTracker() *DefaultTracker {
	now := time.Now()
	return &DefaultTracker{
		state:      StateIdle,
		startTime:  now,
		lastUpdate: now,
	}
}

// SetState updates the tracker's state and message.
func (t *DefaultTracker) SetState(state State, msg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state = state
	t.message = msg
	t.lastUpdate = time.Now()
}

// SetModel updates the tracker's model name.
func (t *DefaultTracker) SetModel(model string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.model = model
}

// GetStatus returns a point-in-time snapshot of the tracker's status.
func (t *DefaultTracker) GetStatus() *Status {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return &Status{
		State:       t.state,
		StateStr:    t.state.String(),
		Model:       t.model,
		Uptime:      time.Since(t.startTime),
		LastUpdated: t.lastUpdate,
		Message:     t.message,
	}
}
