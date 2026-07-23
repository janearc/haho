// Package runner executes one admitted job and lands one JobCompletion.
//
// DIVERGENCE, surfaced: the RFC's shape is hahod spawning a daemonized
// harness process per job, with the chute sandboxed inside it (sections 5
// and 8). This skeleton runs the chute in-process — the harness process
// split and the SBPL profile are the next diff, and nothing in the runner's
// contract changes when they land: it takes a spec, it lands a completion.
package runner

import (
	"context"
	"time"

	"github.com/janearc/haho/gen/jobpb"
	"github.com/janearc/haho/internal/hahod/admission"
	"github.com/janearc/haho/internal/hahod/backend"
)

// Run executes one whole attempt (WHOLE_OR_ERROR is the only served
// framing) and always returns a completion: PRODUCED with the result, or
// FAILED with the specific reason — never fabricated output, never a
// silent nothing.
func Run(ctx context.Context, spec *jobpb.JobSpec) *jobpb.JobCompletion {
	start := time.Now()
	lane := admission.LaneOf(spec.GetBackend().GetKind())

	land := func(outcome jobpb.JobCompletion_Outcome, reason string, res *backend.Result) *jobpb.JobCompletion {
		c := &jobpb.JobCompletion{
			JobId:    spec.GetJobId(),
			Outcome:  outcome,
			Reason:   reason,
			LaneUsed: lane,
			WallMs:   uint64(time.Since(start).Milliseconds()),
		}
		if res != nil {
			c.Result = &jobpb.Payload{Body: &jobpb.Payload_Text{Text: res.Text}}
			c.ModelUsed = res.ModelUsed
			c.TokensIn = res.TokensIn
			c.TokensOut = res.TokensOut
			c.Chunks = 1 // whole-or-error: the work is its own single chunk
		}
		return c
	}

	b, err := backend.New(spec)
	if err != nil {
		return land(jobpb.JobCompletion_FAILED, err.Error(), nil)
	}
	res, err := b.Run(ctx, spec)
	if err != nil {
		return land(jobpb.JobCompletion_FAILED, err.Error(), nil)
	}
	return land(jobpb.JobCompletion_PRODUCED, "", res)
}
