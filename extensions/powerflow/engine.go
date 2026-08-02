// Package powerflow runs power flows and records what they produced.
//
// It owns no mathematics. Every solve crosses into the Rust service through
// pkg/solver, and what this package adds is the platform's half: minting an
// identifier, recording provenance, emitting a domain event, and storing the
// answer so that the number can be produced again later from what was stored
// beside it.
//
// Advisory only. This extension reads a network and reports what it computed.
// Nothing here writes to a control system, and the contract it calls defines
// no method that could. See docs/SAFETY.md.
package powerflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	pfid "github.com/xraph/blackstart/extensions/powerflow/id"
	"github.com/xraph/blackstart/extensions/powerflow/solution"
	"github.com/xraph/blackstart/extensions/powerflow/store"
	"github.com/xraph/blackstart/pkg/hooks"
	"github.com/xraph/blackstart/pkg/id"
	solverv1 "github.com/xraph/blackstart/proto/gen/go/blackstart/solver/v1"
)

// Solver is the subset of pkg/solver.Client this engine needs.
//
// An interface rather than the concrete client so the engine can be tested
// without a running solver. Narrow on purpose: an engine that could reach the
// upload or cache methods would be able to do things its job does not require,
// and every method here is one the tests have to fake.
type Solver interface {
	Solve(
		ctx context.Context,
		model *solverv1.NetworkModel,
		request *solverv1.SolvePowerFlowRequest,
	) (*solverv1.SolvePowerFlowResponse, error)
}

// Engine holds the extension's business logic.
//
// No transport concerns and no storage details: it takes a store interface, so
// it runs against the in-memory implementation with no database at all, and
// the HTTP layer sits above it rather than inside it.
type Engine struct {
	store  store.Store
	solver Solver
	bus    *hooks.Bus

	// now is injectable so a test can pin the time a solution records without
	// sleeping or comparing against a moving target.
	now func() time.Time
}

// NewEngine builds the engine.
//
// The bus may be nil — a CLI invocation has no interest in domain events.
func NewEngine(s store.Store, solver Solver, bus *hooks.Bus) (*Engine, error) {
	if s == nil {
		return nil, errors.New("powerflow: engine needs a store")
	}

	if solver == nil {
		return nil, errors.New("powerflow: engine needs a solver")
	}

	return &Engine{store: s, solver: solver, bus: bus, now: time.Now}, nil
}

// Request is one power flow to run.
type Request struct {
	// Model is the network, in the contract's SI units.
	Model *solverv1.NetworkModel

	// Method is which power flow to run.
	Method solution.Method

	// EnforceReactiveLimits applies generator limits.
	//
	// Off by default, and the caller has to choose: a limited and an unlimited
	// solve answer different questions, and a default would decide that on
	// their behalf.
	EnforceReactiveLimits bool

	// Overrides adjust the model for this solve without re-uploading it.
	Overrides []*solverv1.Override
}

// Validate reports whether a request can be run.
func (r Request) Validate() error {
	if r.Model == nil {
		return errors.New("powerflow: a request needs a network model")
	}

	if len(r.Model.GetBuses()) == 0 {
		return errors.New("powerflow: a model with no buses describes nothing")
	}

	if !r.Method.Valid() {
		return fmt.Errorf("powerflow: %q is not a power-flow method", r.Method)
	}

	return nil
}

// Solve runs a power flow and records the result.
//
// The solution is stored before it is returned, so a caller that acts on a
// number can always point at the record behind it. Storing after returning
// would leave a window where a figure exists in a report and nowhere else.
func (e *Engine) Solve(ctx context.Context, in Request) (solution.Solution, error) {
	if err := in.Validate(); err != nil {
		return solution.Solution{}, err
	}

	request := &solverv1.SolvePowerFlowRequest{
		Method:                methodToWire(in.Method),
		EnforceReactiveLimits: in.EnforceReactiveLimits,
		Overrides:             in.Overrides,
	}

	response, err := e.solver.Solve(ctx, in.Model, request)
	if err != nil {
		return solution.Solution{}, fmt.Errorf("powerflow: solving: %w", err)
	}

	stored, err := e.store.Solutions().Create(ctx, e.fromWire(in, response))
	if err != nil {
		return solution.Solution{}, fmt.Errorf("powerflow: recording the solution: %w", err)
	}

	e.emit(ctx, stored)

	return stored, nil
}

// Get retrieves a recorded solution.
func (e *Engine) Get(ctx context.Context, solutionID id.ID) (solution.Solution, error) {
	return e.store.Solutions().Get(ctx, solutionID)
}

// ListByModel returns what is known about one network, most recent first.
func (e *Engine) ListByModel(
	ctx context.Context, modelHash string, limit int,
) ([]solution.Solution, error) {
	return e.store.Solutions().ListByModel(ctx, modelHash, limit)
}

// fromWire converts a solver response into the domain shape.
//
// This is the per-unit boundary on the read path: the wire is SI and the
// domain is per unit, and the base comes from the model that was solved rather
// than from a constant, so two models on different bases cannot be confused.
func (e *Engine) fromWire(
	in Request, response *solverv1.SolvePowerFlowResponse,
) solution.Solution {
	sBase := in.Model.GetSBaseVa()
	vBase := in.Model.GetVBaseV()

	buses := make([]solution.Bus, len(response.GetBuses()))
	for index, wire := range response.GetBuses() {
		buses[index] = solution.Bus{
			VMagnitude: divide(wire.GetVMagnitudeV(), vBase),
			VAngle:     wire.GetVAngleRad(),
			PInjection: divide(wire.GetPInjectionW(), sBase),
			QInjection: divide(wire.GetQInjectionVar(), sBase),
		}
	}

	branches := make([]solution.Branch, len(response.GetBranches()))
	for index, wire := range response.GetBranches() {
		branches[index] = solution.Branch{
			PFlow: divide(wire.GetPFlowW(), sBase),
			// Loading is already dimensionless — a ratio of flow to rating —
			// so it crosses the boundary untouched. Dividing it by a base
			// would be the easy mistake and is why this is spelled out.
			Loading: wire.GetLoading(),
		}
	}

	return solution.Solution{
		ID:                     pfid.NewSolution(),
		ModelHash:              response.GetModelHash(),
		Method:                 in.Method,
		SolverVersion:          response.GetSolverVersion(),
		EnforcedReactiveLimits: in.EnforceReactiveLimits,
		Buses:                  buses,
		Branches:               branches,
		Iterations:             response.GetIterations(),
		Residual:               divide(response.GetResidualW(), sBase),
		BindingGenerators:      response.GetBindingGenerators(),
		SolvedAt:               e.now().UTC(),
	}
}

// divide converts SI to per unit, leaving the value alone when there is no
// base to divide by.
//
// A zero base means the model did not state one, which Validate on the model
// should have caught. Returning the SI value rather than an infinity keeps the
// failure legible: a voltage of 138000 in a per-unit field is obviously wrong,
// where +Inf could be anything.
func divide(value, base float64) float64 {
	if base == 0 {
		return value
	}

	return value / base
}

func methodToWire(m solution.Method) solverv1.Method {
	if m == solution.MethodDC {
		return solverv1.Method_METHOD_DC
	}

	return solverv1.Method_METHOD_AC
}

// emit publishes a domain event, best effort.
//
// Out of transaction and never fatal: a subscriber that fails must not undo a
// solve that succeeded. The in-transaction audit path is the one that cannot
// be lost, and it is a different mechanism for that reason.
func (e *Engine) emit(ctx context.Context, stored solution.Solution) {
	if e.bus == nil {
		return
	}

	e.bus.Emit(ctx, hooks.Event{
		Name:      "powerflow.solved",
		Extension: "powerflow",
		Entity:    "PowerFlowSolution",
		EntityID:  stored.ID.String(),
		Op:        hooks.OpCreate,
		// Actor and At are filled by Emit from the context, so setting them
		// here would be a second source of truth for both.
		At:      stored.SolvedAt,
		Payload: stored,
	})
}
