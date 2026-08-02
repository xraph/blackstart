package powerflow_test

import (
	"context"
	"testing"

	"github.com/xraph/blackstart/extensions/powerflow"
	"github.com/xraph/blackstart/extensions/powerflow/solution"
	"github.com/xraph/blackstart/extensions/powerflow/store/memory"
	"github.com/xraph/blackstart/pkg/hooks"
	solverv1 "github.com/xraph/blackstart/proto/gen/go/blackstart/solver/v1"
)

// fakeSolver answers with a fixed response and records what it was asked.
//
// The engine's job is everything around the solve — identifiers, conversion,
// storage, events — so the mathematics is stubbed. Running the real solver
// here would make these tests depend on a cargo build and on the numerics,
// neither of which is what is under test.
type fakeSolver struct {
	response *solverv1.SolvePowerFlowResponse
	err      error

	sawRequest *solverv1.SolvePowerFlowRequest
	sawModel   *solverv1.NetworkModel
	calls      int
}

func (f *fakeSolver) Solve(
	_ context.Context,
	model *solverv1.NetworkModel,
	request *solverv1.SolvePowerFlowRequest,
) (*solverv1.SolvePowerFlowResponse, error) {
	f.calls++
	f.sawModel = model
	f.sawRequest = request

	if f.err != nil {
		return nil, f.err
	}

	return f.response, nil
}

func model() *solverv1.NetworkModel {
	return &solverv1.NetworkModel{
		Name:    "two-bus",
		SBaseVa: 100e6,
		VBaseV:  138e3,
		Buses: []*solverv1.Bus{
			{Id: "s", Kind: solverv1.BusKind_BUS_KIND_SLACK},
			{Id: "l", Kind: solverv1.BusKind_BUS_KIND_PQ},
		},
		Branches: []*solverv1.Branch{{Id: "line", FromBus: 0, ToBus: 1, Tap: 1}},
	}
}

// A response in SI, as the contract specifies: volts and watts.
func response() *solverv1.SolvePowerFlowResponse {
	return &solverv1.SolvePowerFlowResponse{
		Buses: []*solverv1.BusResult{
			{VMagnitudeV: 138e3, VAngleRad: 0, PInjectionW: 50e6},
			{VMagnitudeV: 135.24e3, VAngleRad: -0.05, PInjectionW: -50e6},
		},
		Branches: []*solverv1.BranchResult{
			{PFlowW: 50e6, Loading: 0.5},
		},
		Iterations:    4,
		ResidualW:     1e-3,
		SolverVersion: "0.1.0-test",
		ModelHash:     "abc123",
	}
}

func engine(t *testing.T, solver powerflow.Solver, bus *hooks.Bus) *powerflow.Engine {
	t.Helper()

	e, err := powerflow.NewEngine(memory.New(), solver, bus)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	return e
}

func TestAnEngineNeedsBothAStoreAndASolver(t *testing.T) {
	if _, err := powerflow.NewEngine(nil, &fakeSolver{}, nil); err == nil {
		t.Error("NewEngine accepted a nil store")
	}

	if _, err := powerflow.NewEngine(memory.New(), nil, nil); err == nil {
		t.Error("NewEngine accepted a nil solver")
	}
}

// The whole point of this engine: the answer is converted out of SI and into
// per unit, using the base the model declared. A missed conversion produces a
// plausible number on the wrong scale rather than an error, so this asserts
// magnitudes rather than only shape.
func TestTheAnswerComesBackInPerUnit(t *testing.T) {
	solver := &fakeSolver{response: response()}
	e := engine(t, solver, nil)

	got, err := e.Solve(t.Context(), powerflow.Request{
		Model:  model(),
		Method: solution.MethodAC,
	})
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}

	if near := got.Buses[0].VMagnitude; near < 0.99 || near > 1.01 {
		t.Errorf("bus 0 voltage is %v; SI escaped into a per-unit field", near)
	}

	if near := got.Buses[1].VMagnitude; near < 0.97 || near > 0.99 {
		t.Errorf("bus 1 voltage is %v, want about 0.98", near)
	}

	if near := got.Buses[0].PInjection; near < 0.49 || near > 0.51 {
		t.Errorf("slack injection is %v, want 0.5 per unit", near)
	}

	if near := got.Branches[0].PFlow; near < 0.49 || near > 0.51 {
		t.Errorf("branch flow is %v, want 0.5 per unit", near)
	}
}

// Loading is already dimensionless. Dividing it by a base would be the easy
// mistake, and it would silently turn every violation report into nonsense.
func TestLoadingCrossesTheBoundaryUntouched(t *testing.T) {
	solver := &fakeSolver{response: response()}
	e := engine(t, solver, nil)

	got, err := e.Solve(t.Context(), powerflow.Request{
		Model:  model(),
		Method: solution.MethodAC,
	})
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}

	if got.Branches[0].Loading != 0.5 {
		t.Errorf("loading came back as %v, want the 0.5 the solver reported",
			got.Branches[0].Loading)
	}
}

// A solution has to name the solver that produced it and the model it solved,
// or the number cannot be reproduced.
func TestASolutionRecordsWhatProducedIt(t *testing.T) {
	solver := &fakeSolver{response: response()}
	e := engine(t, solver, nil)

	got, err := e.Solve(t.Context(), powerflow.Request{
		Model:                 model(),
		Method:                solution.MethodDC,
		EnforceReactiveLimits: true,
	})
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}

	if got.SolverVersion != "0.1.0-test" {
		t.Errorf("solver version %q", got.SolverVersion)
	}

	if got.ModelHash != "abc123" {
		t.Errorf("model hash %q", got.ModelHash)
	}

	if got.Method != solution.MethodDC {
		t.Errorf("method %q, want the one requested", got.Method)
	}

	if !got.EnforcedReactiveLimits {
		t.Error("the solution did not record that limits were enforced")
	}

	if got.Iterations != 4 {
		t.Errorf("iterations %d, want the solver's count", got.Iterations)
	}

	if got.ID.IsZero() {
		t.Error("the solution has no identifier")
	}

	if got.SolvedAt.IsZero() {
		t.Error("the solution has no timestamp")
	}
}

// Stored before returned, so a caller acting on a number can always point at
// the record behind it.
func TestASolutionIsStoredBeforeItIsReturned(t *testing.T) {
	solver := &fakeSolver{response: response()}
	e := engine(t, solver, nil)

	got, err := e.Solve(t.Context(), powerflow.Request{
		Model:  model(),
		Method: solution.MethodAC,
	})
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}

	stored, err := e.Get(t.Context(), got.ID)
	if err != nil {
		t.Fatalf("the solution that was returned is not in the store: %v", err)
	}

	if stored.ID != got.ID {
		t.Errorf("stored %s, returned %s", stored.ID, got.ID)
	}
}
