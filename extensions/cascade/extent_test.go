package cascade_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/xraph/blackstart/extensions/cascade"
	cid "github.com/xraph/blackstart/extensions/cascade/id"
	"github.com/xraph/blackstart/extensions/cascade/run"
	"github.com/xraph/blackstart/extensions/cascade/store/memory"
	"github.com/xraph/blackstart/extensions/geo/asset"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/geo/shape"
	nmid "github.com/xraph/blackstart/extensions/networkmodel/id"
	nmmodel "github.com/xraph/blackstart/extensions/networkmodel/model"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// fakeModels resolves the one model a test cares about by its content hash,
// the same lookup cascade.Models exists for.
type fakeModels struct {
	model nmmodel.NetworkModel
}

func (f *fakeModels) FindByHash(_ context.Context, hash string) (nmmodel.NetworkModel, error) {
	if hash != f.model.ContentHash {
		return nmmodel.NetworkModel{}, fmt.Errorf("%w: no model for hash %q", bserr.ErrNotFound, hash)
	}

	return f.model, nil
}

// fakeBranches resolves ordinals for one model, the same lookup
// nmmodel.BranchStore.ByOrdinal performs and the same error it wraps an
// unknown ordinal in.
type fakeBranches struct {
	byOrdinal map[string]nmmodel.Branch
}

func (f *fakeBranches) ByOrdinal(_ context.Context, _ id.ID, ordinal string) (nmmodel.Branch, error) {
	branch, ok := f.byOrdinal[ordinal]
	if !ok {
		return nmmodel.Branch{}, fmt.Errorf("%w: model has no %s", bserr.ErrNotFound, ordinal)
	}

	return branch, nil
}

// fakeGeo answers the route and span reads Extent makes. A branch absent
// from routes has no traced route, mirroring geo.Engine.RouteForBranch's own
// bserr.ErrNotFound; a route absent from spans simply has none, mirroring a
// route stored with nothing under it.
type fakeGeo struct {
	routes map[id.ID]asset.Route
	spans  map[id.ID][]asset.Span
}

func (f *fakeGeo) RouteForBranch(_ context.Context, branch id.ID) (asset.Route, error) {
	route, ok := f.routes[branch]
	if !ok {
		return asset.Route{}, fmt.Errorf("%w: branch %s has no traced route", bserr.ErrNotFound, branch)
	}

	return route, nil
}

func (f *fakeGeo) Spans(_ context.Context, route id.ID) ([]asset.Span, error) {
	return f.spans[route], nil
}

// aLine is a two-point path, the minimum shape.Corridor accepts.
func aLine() shape.Line {
	return shape.Line{
		{Lon: -100.000, Lat: 40.000},
		{Lon: -100.010, Lat: 40.010},
	}
}

// extentFixture is one branch tripped in a run, resolved against one model,
// with geometry a test can choose to withhold.
type extentFixture struct {
	modelHash string
	model     nmmodel.NetworkModel
	branch    nmmodel.Branch
	route     asset.Route
	spans     []asset.Span
}

// tripped builds a model with a single branch, "branch-0", ready to be
// tripped in a run. traced controls whether that branch has a route with
// spans behind it.
func tripped(traced bool) extentFixture {
	modelID := nmid.NewModel()
	branchID := nmid.NewBranch()

	fixture := extentFixture{
		modelHash: "model-hash-extent",
		model: nmmodel.NetworkModel{
			ID:          modelID,
			ContentHash: "model-hash-extent",
		},
		branch: nmmodel.Branch{
			ID:      branchID,
			ModelID: modelID,
			Ordinal: "branch-0",
		},
	}

	if traced {
		routeID := gid.NewRoute()
		fixture.route = asset.Route{ID: routeID, CarrierID: branchID}
		fixture.spans = []asset.Span{
			{ID: gid.NewSpan(), RouteID: routeID, Sequence: 1, Geometry: aLine()},
		}
	}

	return fixture
}

// extentEngine builds an engine wired for Extent, backed by a fresh
// in-memory run store and the fakes a fixture describes. It stores the run
// itself, so a test only has to name the events on it.
func extentEngine(t *testing.T, fixture extentFixture, events []run.Event) (*cascade.Engine, id.ID) {
	t.Helper()

	models := &fakeModels{model: fixture.model}
	branches := &fakeBranches{byOrdinal: map[string]nmmodel.Branch{
		fixture.branch.Ordinal: fixture.branch,
	}}

	geoFake := &fakeGeo{
		routes: map[id.ID]asset.Route{},
		spans:  map[id.ID][]asset.Span{},
	}

	if !fixture.route.ID.IsZero() {
		geoFake.routes[fixture.branch.ID] = fixture.route
		geoFake.spans[fixture.route.ID] = fixture.spans
	}

	store := memory.New()

	e, err := cascade.NewEngine(store, &fakeSimulator{}, nil, cascade.WithExtent(models, branches, geoFake))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	runID := cid.NewRun()

	if _, err := store.Runs().Create(t.Context(), run.CascadeRun{
		ID:            runID,
		ModelHash:     fixture.modelHash,
		SolverVersion: "0.1.0-test",
		Initiating:    []string{},
		Events:        events,
	}); err != nil {
		t.Fatalf("seeding the run: %v", err)
	}

	return e, runID
}

// A tripped branch becomes the ground under it.
func TestExtentCoversATrippedBranchesRoute(t *testing.T) {
	fixture := tripped(true)
	events := []run.Event{{Kind: run.EventTrip, Element: "branch-0"}}

	e, runID := extentEngine(t, fixture, events)

	result, err := e.Extent(t.Context(), runID, 25)
	if err != nil {
		t.Fatalf("Extent: %v", err)
	}

	if result.Resolved != 1 {
		t.Errorf("resolved = %d, want 1", result.Resolved)
	}

	if len(result.Unresolved) != 0 {
		t.Errorf("unresolved = %v, want none", result.Unresolved)
	}

	if result.Untraced != 0 {
		t.Errorf("untraced = %d, want 0", result.Untraced)
	}

	if len(result.Area) != 1 {
		t.Fatalf("area has %d polygons, want 1 (one span, one corridor)", len(result.Area))
	}

	if len(result.Area[0]) == 0 {
		t.Error("the corridor for the tripped branch's span is empty")
	}
}

// An element the model does not have is reported rather than dropped: it
// means the run and the model disagree, which is worth knowing.
func TestExtentReportsAnOrdinalTheModelDoesNotHave(t *testing.T) {
	fixture := tripped(true)
	events := []run.Event{{Kind: run.EventTrip, Element: "branch-9"}}

	e, runID := extentEngine(t, fixture, events)

	result, err := e.Extent(t.Context(), runID, 25)
	if err != nil {
		t.Fatalf("Extent: %v", err)
	}

	if len(result.Unresolved) != 1 || result.Unresolved[0] != "branch-9" {
		t.Errorf("unresolved = %v, want [branch-9]", result.Unresolved)
	}

	if result.Resolved != 0 {
		t.Errorf("resolved = %d, want 0", result.Resolved)
	}

	if len(result.Area) != 0 {
		t.Errorf("area has %d polygons, want 0 for an ordinal nobody could resolve", len(result.Area))
	}
}

// Only trips. An overload that never became a trip did not take ground.
func TestExtentIgnoresOverloadsThatDidNotTrip(t *testing.T) {
	fixture := tripped(true)
	events := []run.Event{{Kind: run.EventOverload, Element: "branch-0"}}

	e, runID := extentEngine(t, fixture, events)

	result, err := e.Extent(t.Context(), runID, 25)
	if err != nil {
		t.Fatalf("Extent: %v", err)
	}

	if result.Resolved != 0 {
		t.Errorf("resolved = %d, want 0; an overload was counted as a trip", result.Resolved)
	}

	if result.Untraced != 0 {
		t.Errorf("untraced = %d, want 0", result.Untraced)
	}

	if len(result.Unresolved) != 0 {
		t.Errorf("unresolved = %v, want none", result.Unresolved)
	}

	if len(result.Area) != 0 {
		t.Errorf("area has %d polygons, want 0; an overload took no ground", len(result.Area))
	}
}

// A tripped branch with no route contributes nothing and is counted, because
// a network only partly traced must not read as a small blast radius.
func TestExtentCountsATrippedBranchWithNoRoute(t *testing.T) {
	fixture := tripped(false)
	events := []run.Event{{Kind: run.EventTrip, Element: "branch-0"}}

	e, runID := extentEngine(t, fixture, events)

	result, err := e.Extent(t.Context(), runID, 25)
	if err != nil {
		t.Fatalf("Extent: %v", err)
	}

	if result.Resolved != 1 {
		t.Errorf("resolved = %d, want 1; the branch was found in the model", result.Resolved)
	}

	if result.Untraced != 1 {
		t.Errorf("untraced = %d, want 1", result.Untraced)
	}

	if len(result.Unresolved) != 0 {
		t.Errorf("unresolved = %v, want none; the branch is not unresolved, only untraced", result.Unresolved)
	}

	if len(result.Area) != 0 {
		t.Errorf("area has %d polygons, want 0 for a branch with no route", len(result.Area))
	}
}

// The half-width comes back with the answer, because the area is
// uninterpretable without it.
func TestExtentReportsTheBufferItUsed(t *testing.T) {
	fixture := tripped(true)
	events := []run.Event{{Kind: run.EventTrip, Element: "branch-0"}}

	e, runID := extentEngine(t, fixture, events)

	result, err := e.Extent(t.Context(), runID, 42.5)
	if err != nil {
		t.Fatalf("Extent: %v", err)
	}

	if result.HalfWidthM != 42.5 {
		t.Errorf("half-width = %v, want 42.5", result.HalfWidthM)
	}
}

// A branch that trips twice in one run is one piece of ground: resolved
// once, counted once, buffered once. shape.Cover deliberately does not
// merge, so a duplicate corridor from a repeated trip event is not absorbed
// downstream — it has to not be produced in the first place.
//
// Deleting the `if seen[event.Element] { continue }` guard in Extent would
// fail this test: Resolved would come back 2 for the twice-tripped run
// (instead of matching the once-tripped run's 1), and Area would carry two
// copies of the same corridor instead of one.
func TestExtentDedupesRepeatedTripsOfTheSameBranch(t *testing.T) {
	fixture := tripped(true)

	once := []run.Event{{Kind: run.EventTrip, Element: "branch-0"}}
	twice := []run.Event{
		{Kind: run.EventTrip, Element: "branch-0"},
		{Kind: run.EventTrip, Element: "branch-0"},
	}

	eOnce, runOnce := extentEngine(t, fixture, once)
	eTwice, runTwice := extentEngine(t, fixture, twice)

	resultOnce, err := eOnce.Extent(t.Context(), runOnce, 25)
	if err != nil {
		t.Fatalf("Extent (one trip): %v", err)
	}

	resultTwice, err := eTwice.Extent(t.Context(), runTwice, 25)
	if err != nil {
		t.Fatalf("Extent (two trips, same branch): %v", err)
	}

	if resultTwice.Resolved != resultOnce.Resolved {
		t.Errorf("resolved = %d for a branch tripped twice, want %d (same as tripped once)",
			resultTwice.Resolved, resultOnce.Resolved)
	}

	if resultTwice.Untraced != resultOnce.Untraced {
		t.Errorf("untraced = %d for a branch tripped twice, want %d (same as tripped once)",
			resultTwice.Untraced, resultOnce.Untraced)
	}

	if len(resultTwice.Area) != len(resultOnce.Area) {
		t.Fatalf("area has %d polygons for a branch tripped twice, want %d (same as tripped once); "+
			"the corridor was counted twice", len(resultTwice.Area), len(resultOnce.Area))
	}

	if !reflect.DeepEqual(resultOnce.Area, resultTwice.Area) {
		t.Errorf("area differs between one trip and two trips of the same branch:\n%v\nvs\n%v",
			resultOnce.Area, resultTwice.Area)
	}
}

// Two events naming an ordinal the model does not have is one disagreement,
// not two.
func TestExtentDedupesAnUnresolvedOrdinalNamedTwice(t *testing.T) {
	fixture := tripped(true)
	events := []run.Event{
		{Kind: run.EventTrip, Element: "branch-9"},
		{Kind: run.EventTrip, Element: "branch-9"},
	}

	e, runID := extentEngine(t, fixture, events)

	result, err := e.Extent(t.Context(), runID, 25)
	if err != nil {
		t.Fatalf("Extent: %v", err)
	}

	if len(result.Unresolved) != 1 || result.Unresolved[0] != "branch-9" {
		t.Errorf("unresolved = %v, want [branch-9] exactly once", result.Unresolved)
	}
}
