// Package contingency runs N-1 analyses and records what they found.
//
// It owns no mathematics. The screen runs in Rust and streams back one result
// per contingency; what this package adds is the platform's half — minting an
// identifier, collecting the stream, recording provenance, and storing the run
// so the finding can be produced again from what was stored beside it.
//
// Advisory only. This extension reads a network and reports what it computed.
// Nothing here writes to a control system, and the contract it calls defines
// no method that could. See docs/SAFETY.md.
package contingency

import (
	"context"
	"errors"
	"fmt"
	"time"

	cid "github.com/xraph/blackstart/extensions/contingency/id"
	"github.com/xraph/blackstart/extensions/contingency/store"
	"github.com/xraph/blackstart/extensions/contingency/sweep"
	"github.com/xraph/blackstart/pkg/hooks"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/solver"
	solverv1 "github.com/xraph/blackstart/proto/gen/go/blackstart/solver/v1"
)

// Screener is the subset of pkg/solver.Client this engine needs.
//
// Narrow on purpose: an engine that could reach the upload or solve methods
// would be able to do things its job does not require, and every method here
// is one the tests have to fake.
type Screener interface {
	Screen(
		ctx context.Context,
		model *solverv1.NetworkModel,
		request *solverv1.ScreenContingenciesRequest,
		visit func(*solverv1.ScreenContingenciesResponse) error,
	) error
}

// Engine holds the extension's business logic.
type Engine struct {
	store    store.Store
	screener Screener
	bus      *hooks.Bus

	// solverVersion is what the screen stream cannot tell us.
	//
	// The contract puts version fields on the solve response, not on each
	// streamed screen result — repeating them thousands of times would be
	// noise. So the engine is told once, at construction, and records it with
	// every run. A run that could not name the solver that produced it would
	// not be reproducible.
	solverVersion string

	// now is injectable so a test can pin the times a run records without
	// sleeping or comparing against a moving target.
	now func() time.Time
}

// NewEngine builds the engine.
//
// The bus may be nil — a CLI invocation has no interest in domain events.
func NewEngine(
	s store.Store, screener Screener, solverVersion string, bus *hooks.Bus,
) (*Engine, error) {
	if s == nil {
		return nil, errors.New("contingency: engine needs a store")
	}

	if screener == nil {
		return nil, errors.New("contingency: engine needs a screener")
	}

	if solverVersion == "" {
		return nil, errors.New(
			"contingency: engine needs the solver version; a run that cannot " +
				"name what produced it is not reproducible")
	}

	return &Engine{
		store:         s,
		screener:      screener,
		solverVersion: solverVersion,
		bus:           bus,
		now:           time.Now,
	}, nil
}

// Request is one contingency analysis to run.
type Request struct {
	// Model is the network, in the contract's SI units.
	Model *solverv1.NetworkModel

	// Contingencies is what to evaluate, in the order to evaluate it.
	//
	// Empty means every in-service branch. The ordering is the caller's
	// deliberately: this is where a risk model plugs in, and that substitution
	// is the coupling the whole project exists to demonstrate.
	Contingencies []*solverv1.Contingency

	// Overrides adjust the model for this sweep without re-uploading it.
	Overrides []*solverv1.Override

	// ViolationThreshold is the loading above which a branch is reported.
	// Zero means 1.0 — the rating itself.
	ViolationThreshold float64
}

// Validate reports whether a request can be run.
func (r Request) Validate() error {
	if r.Model == nil {
		return errors.New("contingency: a request needs a network model")
	}

	if len(r.Model.GetBranches()) == 0 {
		return errors.New(
			"contingency: a model with no branches has no contingencies to screen")
	}

	if r.ViolationThreshold < 0 {
		return fmt.Errorf(
			"contingency: a violation threshold of %v is below zero; a branch "+
				"cannot be loaded negatively", r.ViolationThreshold)
	}

	return nil
}

// Screen runs the sweep and records the result.
//
// The run is stored before it is returned, so a caller acting on a finding can
// always point at the record behind it.
func (e *Engine) Screen(ctx context.Context, in Request) (sweep.Run, error) {
	if err := in.Validate(); err != nil {
		return sweep.Run{}, err
	}

	started := e.now().UTC()

	request := &solverv1.ScreenContingenciesRequest{
		Contingencies:      in.Contingencies,
		Overrides:          in.Overrides,
		ViolationThreshold: in.ViolationThreshold,
	}

	var candidates []sweep.Candidate

	err := e.screener.Screen(ctx, in.Model, request,
		func(result *solverv1.ScreenContingenciesResponse) error {
			candidates = append(candidates, fromWire(result, in.Model))

			return nil
		})
	if err != nil {
		return sweep.Run{}, fmt.Errorf("contingency: screening: %w", err)
	}

	// The screen stream carries neither the model hash nor the solver version —
	// those are on the solve response — so the hash is recomputed here from the
	// model that was sent. It is the same value the solver used to find the
	// model, since both derive from the same bytes.
	modelHash, err := solver.HashOnly(in.Model)
	if err != nil {
		return sweep.Run{}, err
	}

	run := sweep.Run{
		ID:            cid.NewRun(),
		ModelHash:     modelHash,
		SolverVersion: e.solverVersion,
		Candidates:    candidates,
		StartedAt:     started,
		FinishedAt:    e.now().UTC(),
	}

	stored, err := e.store.Runs().Create(ctx, run)
	if err != nil {
		return sweep.Run{}, fmt.Errorf("contingency: recording the run: %w", err)
	}

	e.emit(ctx, stored)

	return stored, nil
}

// Get retrieves a recorded run.
func (e *Engine) Get(ctx context.Context, runID id.ID) (sweep.Run, error) {
	return e.store.Runs().Get(ctx, runID)
}

// ListByModel returns what is known about one network, most recent first.
func (e *Engine) ListByModel(
	ctx context.Context, modelHash string, limit int,
) ([]sweep.Run, error) {
	return e.store.Runs().ListByModel(ctx, modelHash, limit)
}

// fromWire converts one streamed screen result into the domain shape.
//
// The per-unit boundary on the read path. The base comes from the model that
// was screened rather than from a constant, so two models on different bases
// cannot be confused.
func fromWire(
	result *solverv1.ScreenContingenciesResponse, model *solverv1.NetworkModel,
) sweep.Candidate {
	sBase := model.GetSBaseVa()

	violations := make([]sweep.Violation, len(result.GetViolations()))
	for index, wire := range result.GetViolations() {
		violations[index] = sweep.Violation{
			Branch: wire.GetBranch(),
			PFlow:  divide(wire.GetPFlowW(), sBase),
			Rating: divide(wire.GetRatingVa(), sBase),
			// Loading is already dimensionless. Dividing it by a base would
			// silently turn every violation report into nonsense.
			Loading: wire.GetLoading(),
		}
	}

	candidate := sweep.Candidate{
		ContingencyID: result.GetContingency().GetId(),
		Branch:        result.GetContingency().GetBranch(),
		Screenability: screenabilityFromWire(result.GetScreenability()),
		Violations:    violations,
	}

	// Only carried across when the wire actually had one. The contract makes
	// this field optional precisely so that "could not be evaluated" is
	// distinguishable from "evaluated at zero", and collapsing the two here
	// would throw that away at the last step.
	if result.WorstLoading != nil {
		candidate.WorstLoading = new(float64)
		*candidate.WorstLoading = result.GetWorstLoading()
	}

	return candidate
}

func screenabilityFromWire(wire solverv1.Screenability) sweep.Screenability {
	switch wire {
	case solverv1.Screenability_SCREENABILITY_SCREENED:
		return sweep.Screened
	case solverv1.Screenability_SCREENABILITY_RADIAL:
		return sweep.Radial
	case solverv1.Screenability_SCREENABILITY_NOT_APPLICABLE:
		return sweep.NotApplicable
	case solverv1.Screenability_SCREENABILITY_UNSPECIFIED:
		// Proto3 cannot distinguish an unset enum from a deliberate zero, and
		// reading this as Screened would report a contingency nobody evaluated
		// as one that came back clean. NotApplicable is wrong too, so it maps
		// to the empty string — which Evaluated() rejects and Concerning()
		// therefore flags.
		return ""
	}

	return ""
}

// divide converts SI to per unit, leaving the value alone when there is no
// base to divide by.
//
// A zero base means the model did not state one. Returning the SI value rather
// than an infinity keeps the failure legible.
func divide(value, base float64) float64 {
	if base == 0 {
		return value
	}

	return value / base
}

// emit publishes a domain event, best effort.
//
// Out of transaction and never fatal: a subscriber that fails must not undo a
// sweep that succeeded.
func (e *Engine) emit(ctx context.Context, stored sweep.Run) {
	if e.bus == nil {
		return
	}

	e.bus.Emit(ctx, hooks.Event{
		Name:      "contingency.screened",
		Extension: "contingency",
		Entity:    "ContingencyRun",
		EntityID:  stored.ID.String(),
		Op:        hooks.OpCreate,
		At:        stored.FinishedAt,
		Payload:   stored,
	})
}
