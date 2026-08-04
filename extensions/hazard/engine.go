// Package hazard turns weather into engineering quantities a conductor cares
// about: what the wind pushes with, what the ice weighs, and how much current
// the line can carry before it sags into something.
//
// The second half of Phase 3, and the platform's first derived quantity. That
// makes it the first thing here to complete ADR-0006's chain end to end — a
// rating traces to the run that produced it, the run to the model version and
// the code commit behind it, and through the weather grids to the dataset
// versions and the sources they were fetched from.
//
// The models themselves live in subpackages and are pure: mechanical for wind
// and ice, thermal for IEEE 738. This package is the part that knows where the
// numbers come from and where they go.
//
// Nothing here guesses a conductor. A span whose branch has no assignment is
// counted unrated and produces no values, because a rating conditioned on an
// invented conductor is worse than no rating: it looks exactly like a real one.
package hazard

import (
	"context"
	"errors"
	"time"

	"github.com/xraph/blackstart/extensions/hazard/conductor"
	"github.com/xraph/blackstart/extensions/hazard/store"
	"github.com/xraph/blackstart/extensions/provenance"
	"github.com/xraph/blackstart/pkg/hooks"
	"github.com/xraph/blackstart/pkg/id"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

// ExtensionName is how this extension is identified to Forge.
const ExtensionName = "hazard"

// The producer identity recorded against every computation.
const (
	// ModelName is what this extension calls itself in the provenance chain.
	ModelName = "bs-hazard"

	// ModelVersion changes whenever a model here would produce a different
	// number from the same weather. It is not the extension's release version
	// and must not be tied to one: a refactor that leaves the physics alone
	// does not invalidate a stored rating, and a corrected drag coefficient
	// does even if nothing else moved.
	ModelVersion = "0.1.0"
)

// Engine holds the extension's business logic.
type Engine struct {
	store store.Store
	bus   *hooks.Bus

	// The compute half. All optional: a read-only deployment, and every test
	// of the read paths, needs none of them — and an engine that demanded a
	// weather source to answer a query would make the read API depend on it.
	weather  Weather
	geometry Geometry
	recorder provenance.Recorder

	// now is injectable so a test can pin the times a computation carries
	// rather than comparing against a moving target.
	now func() time.Time
}

// EngineOption configures the engine.
//
// Named for the engine because the extension already has an Option, and two
// types called the same thing in one package would make every call site ask
// which one it meant.
type EngineOption func(*Engine)

// WithCompute supplies what a computation pass needs.
//
// Separate from the constructor because reading and computing are different
// deployments of the same extension: `bsctl hazard rating` needs a store, and
// nothing else.
//
// The recorder may be nil. A pass without one still computes and still stores
// its values; what it loses is the provenance run, and the computation row
// records that absence as an empty run identifier rather than pretending.
func WithCompute(w Weather, g Geometry, r provenance.Recorder) EngineOption {
	return func(e *Engine) { e.weather, e.geometry, e.recorder = w, g, r }
}

// WithClock replaces the engine's clock.
func WithClock(now func() time.Time) EngineOption {
	return func(e *Engine) {
		if now != nil {
			e.now = now
		}
	}
}

// NewEngine builds the engine.
//
// The bus may be nil — a CLI invocation has no interest in domain events.
func NewEngine(s store.Store, bus *hooks.Bus, opts ...EngineOption) (*Engine, error) {
	if s == nil {
		return nil, errors.New("hazard: engine needs a store")
	}

	e := &Engine{store: s, bus: bus, now: time.Now}

	for _, opt := range opts {
		opt(e)
	}

	return e, nil
}

// Assign binds a branch to a conductor, replacing whatever it had.
func (e *Engine) Assign(ctx context.Context, in conductor.Assignment) error {
	if err := e.store.Assignments().Put(ctx, in); err != nil {
		return err
	}

	e.emit(ctx, "hazard.conductor.assigned", map[string]any{
		"assignment_id":  in.ID.String(),
		"branch_id":      in.BranchID.String(),
		"conductor_code": in.ConductorCode,
		"basis":          in.Basis.String(),
	})

	return nil
}

// Assignment reads one assignment.
func (e *Engine) Assignment(
	ctx context.Context, want id.ID,
) (conductor.Assignment, error) {
	return e.store.Assignments().Get(ctx, want)
}

// AssignmentForBranch finds what a branch is strung with.
func (e *Engine) AssignmentForBranch(
	ctx context.Context, branch id.ID,
) (conductor.Assignment, error) {
	return e.store.Assignments().ForBranch(ctx, branch)
}

// Assignments lists assignments.
func (e *Engine) Assignments(
	ctx context.Context, p pkgstore.Page,
) ([]conductor.Assignment, error) {
	return e.store.Assignments().List(ctx, p)
}

// Unassign removes an assignment, leaving the branch unrated.
func (e *Engine) Unassign(ctx context.Context, want id.ID) error {
	return e.store.Assignments().Delete(ctx, want)
}

// Computation reads one pass.
func (e *Engine) Computation(
	ctx context.Context, want id.ID,
) (store.Computation, error) {
	return e.store.Computations().Get(ctx, want)
}

// Computations lists passes, newest first.
func (e *Engine) Computations(
	ctx context.Context, p pkgstore.Page,
) ([]store.Computation, error) {
	return e.store.Computations().List(ctx, p)
}

// ComputationsForCycle reads every pass over one weather cycle.
func (e *Engine) ComputationsForCycle(
	ctx context.Context, cycle id.ID,
) ([]store.Computation, error) {
	return e.store.Computations().ForCycle(ctx, cycle)
}

// SpanValues reads derived values for one span over a window.
//
// Passing no quantities reads every one. The issuance is required rather than
// defaulted to the newest: a window this method chose would give the same
// request a different answer tomorrow, which is the property a reproducible
// read cannot have.
func (e *Engine) SpanValues(
	ctx context.Context,
	span id.ID, quantities []store.Quantity, issuedAt, from, to time.Time,
) ([]store.SpanValue, error) {
	if span.IsZero() {
		return nil, errors.New("hazard: reading values needs a span")
	}

	if issuedAt.IsZero() {
		return nil, errors.New(
			"hazard: reading values needs an issuance; a rating is only " +
				"meaningful alongside the forecast it was computed from")
	}

	if len(quantities) == 0 {
		quantities = store.Quantities()
	}

	keys := make([]string, 0, len(quantities))
	for _, quantity := range quantities {
		keys = append(keys, store.ValueKey(span, quantity, issuedAt))
	}

	return e.store.Values().Read(ctx, keys, from, to)
}

// emit publishes a domain event, best effort.
func (e *Engine) emit(ctx context.Context, name string, payload map[string]any) {
	if e.bus == nil {
		return
	}

	e.bus.Emit(ctx, hooks.Event{
		Name:      name,
		Extension: ExtensionName,
		Payload:   payload,
	})
}
