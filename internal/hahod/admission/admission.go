// Package admission is the machine's one admission point (RFC section 10):
// budgets are per-machine facts, so exactly one place sees every job. A job
// that cannot be served is REFUSED at admission, loudly, before spending —
// whether the reason is contract-invalid, capacity, or a surface this
// skeleton does not serve yet. Nothing is ever silently queued past a bound
// or silently downgraded.
package admission

import (
	"fmt"
	"os"
	"sync/atomic"

	"github.com/janearc/haho/gen/jobpb"
)

// LaneCfg mirrors one row of the RFC's lane table.
type LaneCfg struct {
	Budget int
	Queue  int
}

// Budgets is the section 10 table. The ANE doors (afm: 1, speech: 3) are
// carried so the table states the whole design, but the ane lane is not
// served by this skeleton and door arithmetic stays unbuilt until it is.
type Budgets struct {
	Metal    LaneCfg
	ANE      LaneCfg
	ANEDoors map[string]int
	API      LaneCfg
	CPU      LaneCfg
}

// Defaults returns the RFC section 10 numbers verbatim.
func Defaults() Budgets {
	return Budgets{
		Metal:    LaneCfg{Budget: 1, Queue: 4},
		ANE:      LaneCfg{Budget: 0, Queue: 8}, // per-door, not per-lane; see ANEDoors
		ANEDoors: map[string]int{"afm": 1, "speech": 3},
		API:      LaneCfg{Budget: 16, Queue: 64},
		CPU:      LaneCfg{Budget: 8, Queue: 32},
	}
}

// LaneOf derives the lane from the backend kind. It is a pure function of
// the contract: a kind/lane mismatch is impossible to express (RFC section 8).
func LaneOf(kind jobpb.Backend_Kind) jobpb.Lane {
	switch kind {
	case jobpb.Backend_MLX:
		return jobpb.Lane_LANE_METAL
	case jobpb.Backend_AFM, jobpb.Backend_SPEECH, jobpb.Backend_TRANSLATION:
		return jobpb.Lane_LANE_ANE
	case jobpb.Backend_ANTHROPIC_COMPATIBLE, jobpb.Backend_OPENAI_COMPATIBLE:
		return jobpb.Lane_LANE_API
	default:
		return jobpb.Lane_LANE_UNSPECIFIED
	}
}

// Refusal is the loud no. Code is stable and machine-readable; Reason names
// the specific fact. A Refusal never carries a secret value — env vars are
// named, never read into an error.
type Refusal struct {
	Code   string
	Reason string
}

func (r *Refusal) Error() string { return fmt.Sprintf("REFUSED (%s): %s", r.Code, r.Reason) }

func refuse(code, format string, args ...any) *Refusal {
	return &Refusal{Code: code, Reason: fmt.Sprintf(format, args...)}
}

// Ticket is an admitted job's claim on its lane. Wait blocks until a run
// slot is held (immediately satisfied when admission found a free slot);
// Release returns the slot. Every admitted ticket must be released exactly
// once.
type Ticket struct {
	Lane   jobpb.Lane
	Queued bool
	gate   *laneGate
}

func (t *Ticket) Wait() {
	if !t.Queued {
		return
	}
	t.gate.slots <- struct{}{}
	t.gate.queued.Add(-1)
	t.Queued = false
}

func (t *Ticket) Release() { <-t.gate.slots }

type laneGate struct {
	slots    chan struct{}
	queued   atomic.Int32
	queueCap int32
}

// Admission validates specs against the contract and the served surface,
// and gates them against lane capacity.
type Admission struct {
	budgets Budgets
	api     *laneGate
	lookup  func(string) (string, bool) // env lookup; swappable in tests
}

func New(b Budgets) *Admission {
	return &Admission{
		budgets: b,
		api:     &laneGate{slots: make(chan struct{}, b.API.Budget), queueCap: int32(b.API.Queue)},
		lookup:  os.LookupEnv,
	}
}

// Admit validates and reserves. The nil-Refusal path returns a Ticket whose
// Wait/Release manage the lane slot. The served surface today is exactly:
// api lane, ANTHROPIC_COMPATIBLE, WHOLE_OR_ERROR, synthesis NONE, cache
// NONE, no filters, text or messages payload. Everything else the contract
// can express is REFUSED by name until the code that serves it exists —
// admission never promises what the machine cannot do.
func (a *Admission) Admit(spec *jobpb.JobSpec) (*Ticket, *Refusal) {
	if spec.GetJobId() == "" {
		return nil, refuse("empty_job_id", "spec carries no job_id; the requestor assigns it")
	}
	kind := spec.GetBackend().GetKind()
	lane := LaneOf(kind)
	if lane == jobpb.Lane_LANE_UNSPECIFIED {
		return nil, refuse("unknown_backend_kind", "backend kind %s does not name a seam", kind)
	}
	if lane != jobpb.Lane_LANE_API {
		return nil, refuse("lane_not_served", "lane %s is not served yet; the api lane is the first door", lane)
	}
	if kind != jobpb.Backend_ANTHROPIC_COMPATIBLE {
		return nil, refuse("backend_kind_not_served", "backend kind %s is not served yet", kind)
	}
	if s := spec.GetFraming().GetStrategy(); s != jobpb.Framing_WHOLE_OR_ERROR {
		return nil, refuse("framing_not_served", "framing strategy %s is not served yet; WHOLE_OR_ERROR is", s)
	}
	if m := spec.GetSynthesis().GetMode(); m != jobpb.Synthesis_MODE_UNSPECIFIED && m != jobpb.Synthesis_NONE {
		return nil, refuse("synthesis_not_served", "synthesis mode %s is not served yet", m)
	}
	if k := spec.GetCache().GetKind(); k != jobpb.Cache_KIND_UNSPECIFIED && k != jobpb.Cache_NONE {
		return nil, refuse("cache_not_served", "cache kind %s is not served yet", k)
	}
	if len(spec.GetInputFilters().GetFilters()) > 0 || len(spec.GetOutputFilters().GetFilters()) > 0 {
		return nil, refuse("filters_not_served", "filter pipelines are not served yet")
	}
	switch spec.GetPayload().GetBody().(type) {
	case *jobpb.Payload_Text, *jobpb.Payload_Messages:
	default:
		return nil, refuse("payload_not_servable", "the api lane serves text and messages payloads; raw is a metal/ane shape")
	}

	// Secret references: the spec names env vars, never values, and each
	// named var must exist non-empty before anything spends (contract,
	// JobSpec.secret_env).
	for logical, envName := range spec.GetSecretEnv() {
		if v, ok := a.lookup(envName); !ok || v == "" {
			return nil, refuse("secret_env_missing", "secret_env %q names env var %s, which is unset or empty", logical, envName)
		}
	}
	if envName := spec.GetBackend().GetTokenEnv(); envName != "" {
		if v, ok := a.lookup(envName); !ok || v == "" {
			return nil, refuse("secret_env_missing", "backend token_env names env var %s, which is unset or empty", envName)
		}
	}

	// Capacity: a free slot runs now; a full lane queues up to the bound; a
	// full queue is REFUSED — never silently held past it.
	select {
	case a.api.slots <- struct{}{}:
		return &Ticket{Lane: lane, gate: a.api}, nil
	default:
	}
	if n := a.api.queued.Add(1); n > a.api.queueCap {
		a.api.queued.Add(-1)
		return nil, refuse("queue_full", "api lane at budget %d with queue %d full",
			a.budgets.API.Budget, a.budgets.API.Queue)
	}
	return &Ticket{Lane: lane, Queued: true, gate: a.api}, nil
}
