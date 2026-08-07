package bsctl

import (
	"testing"

	"github.com/xraph/blackstart/extensions/geo"
	geomemory "github.com/xraph/blackstart/extensions/geo/store/memory"
	"github.com/xraph/blackstart/extensions/networkmodel"
	nmid "github.com/xraph/blackstart/extensions/networkmodel/id"
	"github.com/xraph/blackstart/extensions/networkmodel/model"
	nmstore "github.com/xraph/blackstart/extensions/networkmodel/store"
	nmmemory "github.com/xraph/blackstart/extensions/networkmodel/store/memory"
	"github.com/xraph/blackstart/pkg/id"
)

// newTraceCandidatesFixture builds the two engines traceCandidates needs,
// both backed by in-memory stores — the same pattern newAssumeCandidatesFixture
// uses in hazard_test.go, for the same reason: these are store-backed helpers,
// not command plumbing, and an in-memory store exercises them without booting
// an application.
func newTraceCandidatesFixture(t *testing.T) (*networkmodel.Engine, *geo.Engine, nmstore.Store) {
	t.Helper()

	nmStore := nmmemory.New()

	nm, err := networkmodel.NewEngine(nmStore, nil, nil)
	if err != nil {
		t.Fatalf("networkmodel.NewEngine: %v", err)
	}

	gx, err := geo.NewEngine(geomemory.New(), 0, nil)
	if err != nil {
		t.Fatalf("geo.NewEngine: %v", err)
	}

	return nm, gx, nmStore
}

// putBranch writes a single branch between "bus-1" and "bus-2", plus
// whichever bus rows and placements the caller supplies. A nil bus pointer
// leaves that bus row unwritten entirely — the shape a model imported before
// bus rows existed has for every branch.
func putBranch(
	t *testing.T, st nmstore.Store, modelID, branchID id.ID,
	fromVoltsV, toVoltsV *float64, placeFrom, placeTo bool,
) {
	t.Helper()

	branch := model.Branch{
		ID: branchID, ModelID: modelID, Ordinal: branchID.String(),
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

	var placements []model.Placement

	if placeFrom {
		placements = append(placements, model.Placement{
			ID: nmid.NewPlacement(), ModelID: modelID, Bus: "bus-1",
			Lon: comanche.Lon, Lat: comanche.Lat,
		})
	}

	if placeTo {
		placements = append(placements, model.Placement{
			ID: nmid.NewPlacement(), ModelID: modelID, Bus: "bus-2",
			Lon: dallas.Lon, Lat: dallas.Lat,
		})
	}

	if len(placements) > 0 {
		if err := st.Placements().PutAll(t.Context(), modelID, placements); err != nil {
			t.Fatalf("PutAll placements: %v", err)
		}
	}
}

// A branch whose two ends state different voltages is a transformer: no
// corridor of spans exists between the two terminals of a device that sits
// inside one substation, whatever placements happen to be recorded for its
// ends. It must be skipped and counted separately, not traced.
func TestTraceCandidatesSkipsATransformerAndCountsIt(t *testing.T) {
	t.Parallel()

	nm, gx, st := newTraceCandidatesFixture(t)

	modelID := nmid.NewModel()
	branchID := nmid.NewBranch()

	fromV, toV := 138_000.0, 230_000.0
	putBranch(t, st, modelID, branchID, &fromV, &toV, true, true)

	candidates, noPlacement, alreadyTraced, transformers, err := traceCandidates(
		t.Context(), nm, gx, modelID)
	if err != nil {
		t.Fatalf("traceCandidates: %v", err)
	}

	if len(candidates) != 0 {
		t.Fatalf("got %d candidates, want 0 — a transformer must never be traced", len(candidates))
	}

	if transformers != 1 {
		t.Fatalf("transformers = %d, want 1", transformers)
	}

	if noPlacement != 0 || alreadyTraced != 0 {
		t.Fatalf("noPlacement=%d alreadyTraced=%d, want 0/0", noPlacement, alreadyTraced)
	}
}

// A branch whose two ends state the same voltage is an ordinary conductor
// run and must be traced normally.
func TestTraceCandidatesTracesABranchWithTheSameVoltageAtBothEnds(t *testing.T) {
	t.Parallel()

	nm, gx, st := newTraceCandidatesFixture(t)

	modelID := nmid.NewModel()
	branchID := nmid.NewBranch()

	fromV, toV := 138_000.0, 138_000.0
	putBranch(t, st, modelID, branchID, &fromV, &toV, true, true)

	candidates, noPlacement, alreadyTraced, transformers, err := traceCandidates(
		t.Context(), nm, gx, modelID)
	if err != nil {
		t.Fatalf("traceCandidates: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(candidates))
	}

	if transformers != 0 {
		t.Fatalf("transformers = %d, want 0", transformers)
	}

	if noPlacement != 0 || alreadyTraced != 0 {
		t.Fatalf("noPlacement=%d alreadyTraced=%d, want 0/0", noPlacement, alreadyTraced)
	}
}

// A branch with only one end stating a voltage is not evidence of a
// transformer — absence must never imply a conclusion — so it is traced
// normally, the same rule extensions/hazard/conductor.PlanAssumptions applies.
func TestTraceCandidatesTracesABranchWithOnlyOneEndStatingVoltage(t *testing.T) {
	t.Parallel()

	nm, gx, st := newTraceCandidatesFixture(t)

	modelID := nmid.NewModel()
	branchID := nmid.NewBranch()

	toV := 138_000.0
	putBranch(t, st, modelID, branchID, nil, &toV, true, true)

	candidates, noPlacement, alreadyTraced, transformers, err := traceCandidates(
		t.Context(), nm, gx, modelID)
	if err != nil {
		t.Fatalf("traceCandidates: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1 — one silent end is not a transformer", len(candidates))
	}

	if transformers != 0 {
		t.Fatalf("transformers = %d, want 0", transformers)
	}

	if noPlacement != 0 || alreadyTraced != 0 {
		t.Fatalf("noPlacement=%d alreadyTraced=%d, want 0/0", noPlacement, alreadyTraced)
	}
}

// A model with no bus rows at all — imported before bus rows existed — must
// still trace every branch it can, exactly as before this change. The
// voltage-mismatch check must not turn an absent bus table into a silent
// no-op.
func TestTraceCandidatesWithNoBusRowsTracesEverything(t *testing.T) {
	t.Parallel()

	nm, gx, st := newTraceCandidatesFixture(t)

	modelID := nmid.NewModel()
	branchID := nmid.NewBranch()

	putBranch(t, st, modelID, branchID, nil, nil, true, true)

	candidates, noPlacement, alreadyTraced, transformers, err := traceCandidates(
		t.Context(), nm, gx, modelID)
	if err != nil {
		t.Fatalf("traceCandidates: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(candidates))
	}

	if transformers != 0 {
		t.Fatalf("transformers = %d, want 0 — no bus rows means nothing states a voltage", transformers)
	}

	if noPlacement != 0 || alreadyTraced != 0 {
		t.Fatalf("noPlacement=%d alreadyTraced=%d, want 0/0", noPlacement, alreadyTraced)
	}
}

// Every branch lands in exactly one bucket, and the four counts sum to the
// branch count — the same guarantee bsctl hazard assume prints for its own
// buckets, and the reason trace-model prints "branches total" at all.
func TestTraceCandidatesBucketsSumToBranchCount(t *testing.T) {
	t.Parallel()

	nm, gx, st := newTraceCandidatesFixture(t)

	modelID := nmid.NewModel()

	// PutAll replaces a model's whole set on every call — model.BusStore and
	// model.BranchStore both say so — so every branch, bus, and placement
	// this test needs has to go in through one call each rather than one
	// call per branch, or each later call would erase the branches before it.

	transformerID := nmid.NewBranch()
	noPlacementID := nmid.NewBranch()
	tracedID := nmid.NewBranch()

	fromV, toV := 138_000.0, 230_000.0
	sameV := 69_000.0

	branches := []model.Branch{
		{
			ID: transformerID, ModelID: modelID, Ordinal: "branch-transformer",
			FromBus: "xf-1", ToBus: "xf-2",
		},
		{
			ID: noPlacementID, ModelID: modelID, Ordinal: "branch-no-placement",
			FromBus: "np-1", ToBus: "np-2",
		},
		{
			ID: tracedID, ModelID: modelID, Ordinal: "branch-ready",
			FromBus: "rd-1", ToBus: "rd-2",
		},
	}
	if err := st.Branches().PutAll(t.Context(), modelID, branches); err != nil {
		t.Fatalf("PutAll branches: %v", err)
	}

	buses := []model.Bus{
		{ID: nmid.NewBus(), ModelID: modelID, Ordinal: "xf-1", VNominalV: fromV},
		{ID: nmid.NewBus(), ModelID: modelID, Ordinal: "xf-2", VNominalV: toV},
		{ID: nmid.NewBus(), ModelID: modelID, Ordinal: "rd-1", VNominalV: sameV},
		{ID: nmid.NewBus(), ModelID: modelID, Ordinal: "rd-2", VNominalV: sameV},
	}
	if err := st.Buses().PutAll(t.Context(), modelID, buses); err != nil {
		t.Fatalf("PutAll buses: %v", err)
	}

	placements := []model.Placement{
		{ID: nmid.NewPlacement(), ModelID: modelID, Bus: "xf-1", Lon: comanche.Lon, Lat: comanche.Lat},
		{ID: nmid.NewPlacement(), ModelID: modelID, Bus: "xf-2", Lon: dallas.Lon, Lat: dallas.Lat},
		{ID: nmid.NewPlacement(), ModelID: modelID, Bus: "rd-1", Lon: comanche.Lon, Lat: comanche.Lat},
		{ID: nmid.NewPlacement(), ModelID: modelID, Bus: "rd-2", Lon: dallas.Lon, Lat: dallas.Lat},
	}
	if err := st.Placements().PutAll(t.Context(), modelID, placements); err != nil {
		t.Fatalf("PutAll placements: %v", err)
	}

	candidates, noPlacement, alreadyTraced, transformers, err := traceCandidates(
		t.Context(), nm, gx, modelID)
	if err != nil {
		t.Fatalf("traceCandidates: %v", err)
	}

	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(candidates))
	}

	if transformers != 1 {
		t.Fatalf("transformers = %d, want 1", transformers)
	}

	if noPlacement != 1 {
		t.Fatalf("noPlacement = %d, want 1", noPlacement)
	}

	if alreadyTraced != 0 {
		t.Fatalf("alreadyTraced = %d, want 0", alreadyTraced)
	}

	total := len(candidates) + noPlacement + alreadyTraced + transformers
	if total != 3 {
		t.Fatalf("bucket total = %d, want 3 branches accounted for", total)
	}
}
