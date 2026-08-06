package bsctl

import (
	"math"
	"testing"

	"github.com/xraph/blackstart/extensions/geo"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/geo/shape"
	"github.com/xraph/blackstart/extensions/geo/source"
	geomemory "github.com/xraph/blackstart/extensions/geo/store/memory"
	"github.com/xraph/blackstart/extensions/hazard"
	"github.com/xraph/blackstart/extensions/hazard/conductor"
	hid "github.com/xraph/blackstart/extensions/hazard/id"
	hzmemory "github.com/xraph/blackstart/extensions/hazard/store/memory"
	"github.com/xraph/blackstart/extensions/networkmodel"
	nmid "github.com/xraph/blackstart/extensions/networkmodel/id"
	"github.com/xraph/blackstart/extensions/networkmodel/model"
	nmstore "github.com/xraph/blackstart/extensions/networkmodel/store"
	nmmemory "github.com/xraph/blackstart/extensions/networkmodel/store/memory"
	"github.com/xraph/blackstart/pkg/id"
)

// Two arbitrary substation-like points, used wherever a test needs geometry
// and does not care what it is.
var (
	comanche = shape.Point{Lon: -97.7856, Lat: 32.2983}
	dallas   = shape.Point{Lon: -96.7970, Lat: 32.7767}
)

// Volts on the row, kilovolts in the candidate. The single conversion site.
func TestBranchVoltagesConvertToKilovolts(t *testing.T) {
	t.Parallel()

	from := model.Bus{Ordinal: "bus-1", VNominalV: 138000}
	to := model.Bus{Ordinal: "bus-2", VNominalV: 138000}

	gotFrom, gotTo := branchVoltagesKV(from, to)

	if gotFrom != 138 || gotTo != 138 {
		t.Fatalf("got %v/%v kV, want 138/138", gotFrom, gotTo)
	}
}

// A bus stating nothing converts to zero kV, which PlanAssumptions reads as
// "no voltage" — a skip rather than a default.
func TestABusStatingNothingConvertsToZero(t *testing.T) {
	t.Parallel()

	from := model.Bus{Ordinal: "bus-1", VNominalV: 0}
	to := model.Bus{Ordinal: "bus-2", VNominalV: 138000}

	gotFrom, gotTo := branchVoltagesKV(from, to)

	if gotFrom != 0 {
		t.Fatalf("an unstated bus converted to %v kV, want 0", gotFrom)
	}

	if gotTo != 138 {
		t.Fatalf("got %v kV, want 138", gotTo)
	}
}

// An infinite voltage is refused by canonical.StatedNominalV, which is the one
// definition of "states a voltage". Converting one would produce an infinite
// rating rather than a skip.
func TestAnInfiniteVoltageConvertsToZero(t *testing.T) {
	t.Parallel()

	from := model.Bus{Ordinal: "bus-1", VNominalV: math.Inf(1)}
	to := model.Bus{Ordinal: "bus-2", VNominalV: 138000}

	gotFrom, _ := branchVoltagesKV(from, to)

	if gotFrom != 0 {
		t.Fatalf("an infinite voltage converted to %v kV, want 0", gotFrom)
	}
}

// --- branchEndVoltages / busOrAbsent, against extensions/networkmodel/store/memory ---
//
// These exercise the seam a review flagged as untested: not-found versus a
// genuine read failure, at both the branch level and the bus level, and that
// both ends flow through the one conversion site.

// newBranchFixture stores one branch, and whichever of its two bus rows the
// caller supplies. Passing a nil pointer for an end leaves that bus row
// unwritten entirely — the shape a model imported before migration 0004 has
// for every one of its branches, and the shape a partially-projected model
// has for whichever end it is missing.
func newBranchFixture(t *testing.T, fromVoltsV, toVoltsV *float64) (nmstore.Store, id.ID) {
	t.Helper()

	st := nmmemory.New()
	modelID := nmid.NewModel()
	branchID := nmid.NewBranch()

	branch := model.Branch{
		ID: branchID, ModelID: modelID, Ordinal: "branch-1",
		FromBus: "bus-1", ToBus: "bus-2",
	}

	if err := st.Branches().PutAll(t.Context(), modelID, []model.Branch{branch}); err != nil {
		t.Fatalf("PutAll branches: %v", err)
	}

	var buses []model.Bus

	if fromVoltsV != nil {
		buses = append(buses, model.Bus{
			ID: nmid.NewBus(), ModelID: modelID, Ordinal: "bus-1", VNominalV: *fromVoltsV,
		})
	}

	if toVoltsV != nil {
		buses = append(buses, model.Bus{
			ID: nmid.NewBus(), ModelID: modelID, Ordinal: "bus-2", VNominalV: *toVoltsV,
		})
	}

	if len(buses) > 0 {
		if err := st.Buses().PutAll(t.Context(), modelID, buses); err != nil {
			t.Fatalf("PutAll buses: %v", err)
		}
	}

	return st, branchID
}

// A branch row that is not stored at all — the route names a br_ carrier
// nothing in this store knows, which RouteForBranch's own doc says means the
// model it belonged to is gone. That is a gap in the record, not a fault in
// this pass: it must not fail the whole run over one branch.
func TestBranchEndVoltagesWhenTheBranchRowIsAbsent(t *testing.T) {
	t.Parallel()

	st := nmmemory.New()

	voltageKV, farEndKV, err := branchEndVoltages(t.Context(), st, nmid.NewBranch())
	if err != nil {
		t.Fatalf("branchEndVoltages: %v, want no error for an absent branch", err)
	}

	if voltageKV != 0 || farEndKV != 0 {
		t.Fatalf("got %v/%v kV, want 0/0", voltageKV, farEndKV)
	}
}

// A model imported before migration 0004 has a branch row but no bus rows at
// all. That must land the branch in NoVoltage downstream — a zero-valued
// candidate, not an error.
func TestBranchEndVoltagesWithNoBusRowsAtAll(t *testing.T) {
	t.Parallel()

	st, branchID := newBranchFixture(t, nil, nil)

	voltageKV, farEndKV, err := branchEndVoltages(t.Context(), st, branchID)
	if err != nil {
		t.Fatalf("branchEndVoltages: %v, want no error for a pre-migration model", err)
	}

	if voltageKV != 0 || farEndKV != 0 {
		t.Fatalf("got %v/%v kV, want 0/0", voltageKV, farEndKV)
	}
}

// Both ends present and different, both converted through the one site: this
// is the ordinary transformer-detection shape, and it depends on the two
// conversions agreeing bit-for-bit.
func TestBranchEndVoltagesWithBothEndsStated(t *testing.T) {
	t.Parallel()

	fromV, toV := 138_000.0, 230_000.0
	st, branchID := newBranchFixture(t, &fromV, &toV)

	voltageKV, farEndKV, err := branchEndVoltages(t.Context(), st, branchID)
	if err != nil {
		t.Fatalf("branchEndVoltages: %v", err)
	}

	if voltageKV != 138 || farEndKV != 230 {
		t.Fatalf("got %v/%v kV, want 138/230", voltageKV, farEndKV)
	}
}

// Exactly one end states a voltage — here the from-bus row is missing
// entirely, the to-bus states 138 kV. The candidate must carry the stated
// end's voltage regardless of which end it came from. Before this fix,
// VoltageKV was unconditionally the from-end's, which was 0 here: this branch
// would have landed in NoVoltage despite one of its ends stating 138 kV.
func TestBranchEndVoltagesAssumesFromTheStatedEndWhenTheOtherIsAbsent(t *testing.T) {
	t.Parallel()

	toV := 138_000.0
	st, branchID := newBranchFixture(t, nil, &toV)

	voltageKV, farEndKV, err := branchEndVoltages(t.Context(), st, branchID)
	if err != nil {
		t.Fatalf("branchEndVoltages: %v", err)
	}

	if voltageKV != 138 {
		t.Fatalf("candidate voltage = %v kV, want 138 — the stated end, "+
			"whichever end it was", voltageKV)
	}

	if farEndKV != 0 {
		t.Fatalf("far-end voltage = %v kV, want 0", farEndKV)
	}
}

// Note on a genuine read failure: extensions/networkmodel/store/memory has no
// way to force Branches().Get or Buses().ByOrdinal to return anything but
// bserr.ErrNotFound or a nil error — there is no injectable failure mode, and
// the review that asked for this coverage said explicitly not to invent a
// mock for it. branchEndVoltages' error-propagation path (the `return 0, 0,
// fmt.Errorf(...)` branches, as opposed to the `errors.Is(err,
// bserr.ErrNotFound)` branches covered above) is therefore exercised by
// inspection only: it forwards any non-not-found error from Branches().Get,
// Buses().ByOrdinal("from"), or Buses().ByOrdinal("to") unchanged, wrapped.

// --- assumeCandidates, against real (memory-backed) engines ---
//
// These cover what only exists once all three engines are wired together:
// that an already-assigned carrier's voltage is never looked up (skipping the
// lookup rather than merely discarding its result), that such a carrier still
// reaches PlanAssumptions and lands in AlreadyAssigned, and that a branch with
// one silent end still gets a conductor assumed regardless of which end it
// was that stayed silent.

func newAssumeCandidatesFixture(t *testing.T) (
	*hazard.Engine, *geo.Engine, *networkmodel.Engine, nmstore.Store,
) {
	t.Helper()

	hz, err := hazard.NewEngine(hzmemory.New(), nil)
	if err != nil {
		t.Fatalf("hazard.NewEngine: %v", err)
	}

	gx, err := geo.NewEngine(geomemory.New(), 0, nil)
	if err != nil {
		t.Fatalf("geo.NewEngine: %v", err)
	}

	nmStore := nmmemory.New()

	nm, err := networkmodel.NewEngine(nmStore, nil, nil)
	if err != nil {
		t.Fatalf("networkmodel.NewEngine: %v", err)
	}

	return hz, gx, nm, nmStore
}

// assignSomething gives a carrier a valid, arbitrary assignment — the exact
// conductor does not matter to these tests, only that AssignmentForCarrier
// then finds one.
func assignSomething(t *testing.T, hz *hazard.Engine, carrier id.ID) {
	t.Helper()

	code, err := conductor.AssumeByVoltage(138)
	if err != nil {
		t.Fatalf("AssumeByVoltage: %v", err)
	}

	err = hz.Assign(t.Context(), conductor.Assignment{
		ID: hid.NewAssignment(), CarrierID: carrier,
		ConductorCode: code, Basis: conductor.Surveyed,
	})
	if err != nil {
		t.Fatalf("Assign: %v", err)
	}
}

// A circuit route whose circuit was never registered — gx.Circuit(ctx,
// circuitID) fails for it with bserr.ErrNotFound. Without the already-assigned
// guard, assumeCandidates would call gx.Circuit anyway and fail the whole
// pass over a value PlanAssumptions was never going to read. With the guard,
// it must succeed, and the candidate must still show Assigned and an untouched
// (zero) voltage — proof the lookup did not run, not just that its result
// was discarded.
func TestAssumeCandidatesSkipsTheLookupForAnAlreadyAssignedCircuit(t *testing.T) {
	t.Parallel()

	hz, gx, nm, _ := newAssumeCandidatesFixture(t)

	circuitID := gid.NewCircuit()

	if _, err := gx.Trace(t.Context(), geo.TraceRequest{
		BranchID: circuitID, From: comanche, To: dallas,
	}); err != nil {
		t.Fatalf("Trace: %v", err)
	}

	assignSomething(t, hz, circuitID)

	candidates, err := assumeCandidates(t.Context(), hz, gx, nm, 100)
	if err != nil {
		t.Fatalf("assumeCandidates: %v — an already-assigned circuit's lookup "+
			"must be skipped, not attempted and failed", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(candidates))
	}

	got := candidates[0]

	if !got.Assigned {
		t.Errorf("candidate.Assigned = false, want true")
	}

	if got.VoltageKV != 0 {
		t.Errorf("candidate.VoltageKV = %v, want 0 — the circuit lookup should "+
			"never have run", got.VoltageKV)
	}
}

// An already-assigned branch must still reach the plan and land in
// AlreadyAssigned — the guard that stops its lookup from running must not
// also drop it from the candidate list.
func TestAssumeCandidatesKeepsAnAlreadyAssignedBranchInThePlan(t *testing.T) {
	t.Parallel()

	hz, gx, nm, nmStore := newAssumeCandidatesFixture(t)

	modelID := nmid.NewModel()
	branchID := nmid.NewBranch()

	branch := model.Branch{
		ID: branchID, ModelID: modelID, Ordinal: "branch-1",
		FromBus: "bus-1", ToBus: "bus-2",
	}

	if err := nmStore.Branches().PutAll(t.Context(), modelID, []model.Branch{branch}); err != nil {
		t.Fatalf("PutAll branches: %v", err)
	}

	// No bus rows written at all — deliberately. An already-assigned branch's
	// voltage must never be read, so a model with nothing projected for it
	// must not matter either.

	if _, err := gx.Trace(t.Context(), geo.TraceRequest{
		BranchID: branchID, From: comanche, To: dallas,
	}); err != nil {
		t.Fatalf("Trace: %v", err)
	}

	assignSomething(t, hz, branchID)

	candidates, err := assumeCandidates(t.Context(), hz, gx, nm, 100)
	if err != nil {
		t.Fatalf("assumeCandidates: %v", err)
	}

	plan := conductor.PlanAssumptions(candidates)

	if len(plan.AlreadyAssigned) != 1 || plan.AlreadyAssigned[0] != branchID {
		t.Fatalf("plan.AlreadyAssigned = %v, want [%s]", plan.AlreadyAssigned, branchID)
	}

	if len(plan.Assume) != 0 || len(plan.NoVoltage) != 0 || len(plan.Transformers) != 0 {
		t.Fatalf("an already-assigned branch leaked into another bucket: %+v", plan)
	}
}

// A branch whose from-bus row is absent and whose to-bus states 138 kV must
// still be assumable — the candidate's voltage must come from whichever end
// stated one. Under the unconditional "VoltageKV = fromKV" this candidate
// carried 0 kV and PlanAssumptions put it in NoVoltage; this test fails under
// that version and passes under the fix.
func TestAssumeCandidatesPrefersTheStatedEndForABranch(t *testing.T) {
	t.Parallel()

	hz, gx, nm, nmStore := newAssumeCandidatesFixture(t)

	modelID := nmid.NewModel()
	branchID := nmid.NewBranch()

	branch := model.Branch{
		ID: branchID, ModelID: modelID, Ordinal: "branch-1",
		FromBus: "bus-1", ToBus: "bus-2",
	}

	if err := nmStore.Branches().PutAll(t.Context(), modelID, []model.Branch{branch}); err != nil {
		t.Fatalf("PutAll branches: %v", err)
	}

	// Only bus-2 (the to-end) is projected. bus-1 (the from-end) has no row.
	if err := nmStore.Buses().PutAll(t.Context(), modelID, []model.Bus{
		{ID: nmid.NewBus(), ModelID: modelID, Ordinal: "bus-2", VNominalV: 138_000},
	}); err != nil {
		t.Fatalf("PutAll buses: %v", err)
	}

	if _, err := gx.Trace(t.Context(), geo.TraceRequest{
		BranchID: branchID, From: comanche, To: dallas,
	}); err != nil {
		t.Fatalf("Trace: %v", err)
	}

	candidates, err := assumeCandidates(t.Context(), hz, gx, nm, 100)
	if err != nil {
		t.Fatalf("assumeCandidates: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(candidates))
	}

	got := candidates[0]

	if got.VoltageKV != 138 {
		t.Fatalf("candidate.VoltageKV = %v, want 138 — the stated end, "+
			"regardless of which end it is", got.VoltageKV)
	}

	if got.FarEndVoltageKV != 0 {
		t.Fatalf("candidate.FarEndVoltageKV = %v, want 0", got.FarEndVoltageKV)
	}

	plan := conductor.PlanAssumptions(candidates)
	if len(plan.Assume) != 1 {
		t.Fatalf("plan.Assume has %d entries, want 1 — plan: %+v", len(plan.Assume), plan)
	}
}

// A circuit's FarEndVoltageKV is left at its zero value — a circuit has one
// voltage and no far end, and the transformer check must never see it as a
// stated 0 kV far end.
func TestAssumeCandidatesLeaveFarEndVoltageZeroForACircuit(t *testing.T) {
	t.Parallel()

	hz, gx, nm, _ := newAssumeCandidatesFixture(t)

	_, _, err := gx.ImportLine(t.Context(), source.Line{
		ExternalID: "test-circuit-1",
		Source:     "test",
		Path:       shape.Line{comanche, dallas},
		VoltageKV:  138,
	})
	if err != nil {
		t.Fatalf("ImportLine: %v", err)
	}

	candidates, err := assumeCandidates(t.Context(), hz, gx, nm, 100)
	if err != nil {
		t.Fatalf("assumeCandidates: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(candidates))
	}

	got := candidates[0]

	if got.VoltageKV != 138 {
		t.Fatalf("candidate.VoltageKV = %v, want 138", got.VoltageKV)
	}

	if got.FarEndVoltageKV != 0 {
		t.Fatalf("candidate.FarEndVoltageKV = %v, want 0 for a circuit", got.FarEndVoltageKV)
	}
}
