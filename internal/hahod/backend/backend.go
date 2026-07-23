// Package backend is the model seam. Pluggability is exactly the enum
// surface (RFC section 8): each Backend.Kind maps to one implementation
// through the registry here — adding a backend is one implementation and
// one registration, and no requestor changes.
package backend

import (
	"context"
	"fmt"

	"github.com/janearc/haho/gen/jobpb"
)

// Result is one whole attempt's product, in the terms JobCompletion
// surfaces: text out, the model that actually answered, and token counts
// when the seam reports them (-1 when it does not — absent is not zero).
type Result struct {
	Text      string
	ModelUsed string
	TokensIn  int32
	TokensOut int32
}

// Backend executes one whole attempt of a spec's payload against the model
// its descriptor names. WHOLE_OR_ERROR semantics live here: one attempt, no
// retry, no resize — any error is the caller's to classify and land as
// FAILED, never as fabricated output.
type Backend interface {
	Run(ctx context.Context, spec *jobpb.JobSpec) (*Result, error)
}

// Constructor builds a Backend from a spec's descriptor. Descriptors are
// data; the client is constructed at launch (RFC section 8).
type Constructor func(spec *jobpb.JobSpec) (Backend, error)

var registry = map[jobpb.Backend_Kind]Constructor{}

// Register wires a kind to its constructor. Called from implementation
// init(); a duplicate registration is a programming error and panics at
// startup, never at admission time.
func Register(kind jobpb.Backend_Kind, c Constructor) {
	if _, dup := registry[kind]; dup {
		panic(fmt.Sprintf("backend: duplicate registration for %s", kind))
	}
	registry[kind] = c
}

// New constructs the backend a spec names. An unregistered kind is an
// error here as a backstop; admission refuses it long before this.
func New(spec *jobpb.JobSpec) (Backend, error) {
	c, ok := registry[spec.GetBackend().GetKind()]
	if !ok {
		return nil, fmt.Errorf("backend: kind %s has no implementation", spec.GetBackend().GetKind())
	}
	return c(spec)
}
