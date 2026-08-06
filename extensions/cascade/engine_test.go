package cascade_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/cascade"
	"github.com/xraph/blackstart/extensions/cascade/store/memory"
	"github.com/xraph/blackstart/pkg/hooks"
	solverv1 "github.com/xraph/blackstart/proto/gen/go/blackstart/solver/v1"
)

// fakeSimulator returns a fixed response and records what it was asked.
//
// The mathematics is stubbed: the engine's job is converting units and
// recording provenance, and running the real cascade would make these tests
// depend on a cargo build and on the numerics.
type fakeSimulator struct {
	response *solverv1.SimulateCascadeResponse
	err      error

	sawRequest *solverv1.SimulateCascadeRequest
	calls      int
}

func (f *fakeSimulator) Simulate(
	_ context.Context,
	_ *solverv1.NetworkModel,
	request *solverv1.SimulateCascadeRequest,
) (*solverv1.SimulateCascadeResponse, error) {
	f.calls++
	f.sawRequest = request

	if f.err != nil {
		return nil, f.err
	}

	return f.response, nil
}

// model needs only what Request.Validate checks: a non-empty branch list.
func model() *solverv1.NetworkModel {
	return &solverv1.NetworkModel{
		SBaseVa:  100e6,
		Branches: []*solverv1.Branch{{Id: "s-a", FromBus: 0, ToBus: 1, Tap: 1}},
	}
}

// settledResponse is the minimum a response needs to be storable: a run
// without a model hash or a solver version fails run.CascadeRun.Validate, so every
// fake response a happy-path test uses has to carry both.
func settledResponse() *solverv1.SimulateCascadeResponse {
	return &solverv1.SimulateCascadeResponse{
		Outcome:       solverv1.CascadeOutcome_CASCADE_OUTCOME_SETTLED,
		SolverVersion: "0.1.0-test",
		ModelHash:     "deadbeef",
	}
}

func engine(t *testing.T, simulator cascade.Simulator, bus *hooks.Bus) *cascade.Engine {
	t.Helper()

	e, err := cascade.NewEngine(memory.New(), simulator, bus)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	return e
}

// A simulation failure must leave nothing behind. A partial run would sit in
// the store looking like a completed simulation. Proven by giving the same
// engine a working simulator afterward: if the failed attempt had left
// something, this model would show two runs instead of one.
func TestASimulationFailureStoresNothing(t *testing.T) {
	simulator := &fakeSimulator{err: errors.New("the solver fell over")}
	e := engine(t, simulator, nil)

	_, err := e.Simulate(t.Context(), cascade.Request{Model: model()})
	if err == nil {
		t.Fatal("Simulate succeeded against a failing simulator")
	}

	if !strings.Contains(err.Error(), "fell over") {
		t.Errorf("the simulator's own error was lost: %v", err)
	}

	simulator.err = nil
	simulator.response = settledResponse()

	result, err := e.Simulate(t.Context(), cascade.Request{Model: model()})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}

	found, err := e.ListByModel(t.Context(), result.ModelHash, 0)
	if err != nil {
		t.Fatalf("ListByModel: %v", err)
	}

	if len(found) != 1 {
		t.Errorf("got %d runs for this model, want 1; the failed attempt left something behind", len(found))
	}
}

// A cascade produced from a model resolved by canonical hash is found again by
// that same hash — the lookup a scenario's operator has, and the one that
// returned nothing before the run carried two.
//
// Two mutations make it fail: dropping the CanonicalHash assignment in
// Simulate, and reverting the memory store's filter to compare only ModelHash.
// Both were run; both fail on "found 0 cascades".
func TestACascadeIsFoundByTheCanonicalHashItWasResolvedBy(t *testing.T) {
	const canonical = "canonical-side-of-the-same-network"

	simulator := &fakeSimulator{response: settledResponse()}
	e := engine(t, simulator, nil)

	result, err := e.Simulate(t.Context(), cascade.Request{
		Model:         model(),
		CanonicalHash: canonical,
	})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}

	if result.CanonicalHash == nil || *result.CanonicalHash != canonical {
		t.Fatalf("the run recorded canonical hash %v, want %q", result.CanonicalHash, canonical)
	}

	// The solver's own hash is a different string, so a store that compared
	// only that one cannot pass by coincidence.
	if result.ModelHash == canonical {
		t.Fatal("the solver's hash equals the canonical hash, so this test proves nothing")
	}

	found, err := e.ListByModel(t.Context(), canonical, 0)
	if err != nil {
		t.Fatalf("ListByModel: %v", err)
	}

	if len(found) != 1 || found[0].ID != result.ID {
		t.Fatalf("found %d cascades for the canonical hash, want the one just simulated", len(found))
	}

	// And the solver-side hash keeps working: every run recorded before this
	// field existed has only that one.
	bySolver, err := e.ListByModel(t.Context(), result.ModelHash, 0)
	if err != nil {
		t.Fatalf("ListByModel by the solver's hash: %v", err)
	}

	if len(bySolver) != 1 || bySolver[0].ID != result.ID {
		t.Fatalf("found %d cascades for the solver's hash, want the one just simulated", len(bySolver))
	}
}

// A caller with no canonical hash records absence, not a blank one.
//
// Replacing optionalHash with a plain &in.CanonicalHash makes this fail —
// Validate refuses the pointer to "" before the run reaches the store.
func TestACascadeWithNoCanonicalHashRecordsNothingRatherThanEmptiness(t *testing.T) {
	simulator := &fakeSimulator{response: settledResponse()}
	e := engine(t, simulator, nil)

	result, err := e.Simulate(t.Context(), cascade.Request{Model: model()})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}

	if result.CanonicalHash != nil {
		t.Errorf("the run invented a canonical hash %q from a caller that supplied none",
			*result.CanonicalHash)
	}

	found, err := e.ListByModel(t.Context(), "", 0)
	if err != nil {
		t.Fatalf("ListByModel: %v", err)
	}

	if len(found) != 0 {
		t.Errorf("the empty hash matched %d runs; a run that does not know its "+
			"canonical hash must not answer to one of no characters", len(found))
	}
}

func TestARunIsStoredBeforeItIsReturned(t *testing.T) {
	simulator := &fakeSimulator{response: settledResponse()}
	e := engine(t, simulator, nil)

	result, err := e.Simulate(t.Context(), cascade.Request{Model: model()})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}

	stored, err := e.Get(t.Context(), result.ID)
	if err != nil {
		t.Fatalf("the run that was returned is not in the store: %v", err)
	}

	if stored.ID != result.ID {
		t.Errorf("stored %s, returned %s", stored.ID, result.ID)
	}
}

// SI to per unit, the same crossing extensions/contingency tests for
// violations. load_lost_w of 50 MW against a 100 MVA base is 0.5 pu.
func TestLoadLostComesBackInPerUnit(t *testing.T) {
	response := settledResponse()
	response.LoadLostW = 50e6

	simulator := &fakeSimulator{response: response}

	result, err := engine(t, simulator, nil).Simulate(t.Context(), cascade.Request{Model: model()})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}

	if result.LoadLost != 0.5 {
		t.Errorf("load lost is %v, want 0.5 per unit; SI escaped", result.LoadLost)
	}
}

// A run must name the model and the solver, or its result cannot be
// reproduced. Both are read from the response, not from engine configuration
// — SimulateCascadeResponse states them, unlike the contingency screen.
func TestARunRecordsWhatProducedIt(t *testing.T) {
	response := settledResponse()
	response.SolverVersion = "0.4.2-cascade"
	response.ModelHash = "abc123def456"

	simulator := &fakeSimulator{response: response}

	result, err := engine(t, simulator, nil).Simulate(t.Context(), cascade.Request{Model: model()})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}

	if result.SolverVersion != "0.4.2-cascade" {
		t.Errorf("solver version %q", result.SolverVersion)
	}

	if result.ModelHash != "abc123def456" {
		t.Errorf("model hash %q", result.ModelHash)
	}

	if result.ID.IsZero() {
		t.Error("the run has no identifier")
	}

	if result.StartedAt.IsZero() || result.FinishedAt.IsZero() {
		t.Error("the run is not bounded in time")
	}
}

// Proto3 cannot distinguish an unset enum from a deliberate zero. Reading
// CASCADE_OUTCOME_UNSPECIFIED as OutcomeSettled would report a cascade
// nobody categorized as one that finished cleanly — of the four tables in
// engine.go, this is the direction that matters most: it hides a run whose
// ending was never determined behind the calmest label available.
func TestAnUnspecifiedOutcomeIsNotReadAsSettled(t *testing.T) {
	response := settledResponse()
	response.Outcome = solverv1.CascadeOutcome_CASCADE_OUTCOME_UNSPECIFIED

	simulator := &fakeSimulator{response: response}

	result, err := engine(t, simulator, nil).Simulate(t.Context(), cascade.Request{Model: model()})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}

	if result.Outcome != "" {
		t.Errorf("an unset outcome came back as %q, want empty", result.Outcome)
	}
}
