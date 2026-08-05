package geo_test

import (
	"context"
	"testing"

	"github.com/xraph/blackstart/extensions/geo"
	"github.com/xraph/blackstart/extensions/geo/asset"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/geo/shape"
	"github.com/xraph/blackstart/extensions/geo/source"
	"github.com/xraph/blackstart/pkg/id"
)

// A digitized right-of-way with a real corner in it.
func rightOfWay() shape.Line {
	return shape.Line{
		{Lon: -101.76, Lat: 34.54},
		{Lon: -102.10, Lat: 34.20},
		{Lon: -102.38, Lat: 34.20},
	}
}

// asStructures wraps bare positions as source structures that say nothing
// about what they are, for tests that only care about the geometry.
func asStructures(positions shape.Line) []source.Structure {
	structures := make([]source.Structure, len(positions))
	for index, position := range positions {
		structures[index] = source.Structure{Position: position}
	}

	return structures
}

// threeStructures is what a source reports when it knows what its structures
// are, not just where they stand.
func threeStructures() []source.Structure {
	return []source.Structure{
		{Position: shape.Point{Lon: -97.80, Lat: 30.3}, Kind: "tower", Material: "steel"},
		{Position: shape.Point{Lon: -97.7979, Lat: 30.3}, Kind: "tower"},
		{Position: shape.Point{Lon: -97.7958, Lat: 30.3}, Kind: "pole"},
	}
}

func TestImportWithoutStructuresIsRoutedNotSurveyed(t *testing.T) {
	e := engine(t)

	route, err := e.Import(t.Context(), geo.ImportRequest{
		CarrierID: id.MustNew("circ"),
		Path:      rightOfWay(),
		SpanM:     400,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	// The path came from a source; the structures did not. Calling this
	// Surveyed would assert towers nobody recorded.
	if route.Provenance != asset.Routed {
		t.Errorf("provenance = %q, want %q", route.Provenance, asset.Routed)
	}

	if route.Provenance.StructuresAreReal() {
		t.Error("an imported polyline must not claim real structures")
	}

	if !route.Provenance.PathIsReal() {
		t.Error("the right-of-way is real and the route should say so")
	}
}

func TestImportWithStructuresIsSurveyed(t *testing.T) {
	e := engine(t)

	route, err := e.Import(t.Context(), geo.ImportRequest{
		CarrierID:  id.MustNew("circ"),
		Path:       rightOfWay(),
		Structures: asStructures(rightOfWay()),
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if route.Provenance != asset.Surveyed {
		t.Errorf("provenance = %q, want %q", route.Provenance, asset.Surveyed)
	}
}

// Recorded structures become the vertices directly. Resampling between them
// would insert imaginary towers among real ones while still claiming Surveyed.
func TestImportDoesNotResampleRecordedStructures(t *testing.T) {
	e := engine(t)
	structures := rightOfWay()

	route, err := e.Import(t.Context(), geo.ImportRequest{
		CarrierID:  id.MustNew("circ"),
		Path:       structures,
		Structures: asStructures(structures),
		SpanM:      50, // would insert many points if it were applied
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	spans, err := e.Spans(t.Context(), route.ID)
	if err != nil {
		t.Fatalf("Spans: %v", err)
	}

	if len(spans) != len(structures)-1 {
		t.Errorf("got %d spans from %d structures, want %d — structures were resampled",
			len(spans), len(structures), len(structures)-1)
	}
}

// The routed path must keep its corner rather than being flattened.
func TestImportKeepsTheDigitizedCorner(t *testing.T) {
	e := engine(t)

	route, err := e.Import(t.Context(), geo.ImportRequest{
		CarrierID: id.MustNew("circ"),
		Path:      rightOfWay(),
		SpanM:     400,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	ends := rightOfWay()
	chord := ends[0].DistanceTo(ends[len(ends)-1])

	if route.LengthM <= chord*1.02 {
		t.Errorf("route length %.0f m is not meaningfully longer than the chord %.0f m; "+
			"the right-of-way was flattened onto a great circle", route.LengthM, chord)
	}
}

func TestImportRefusesAPathThatCarriesNothing(t *testing.T) {
	e := engine(t)

	if _, err := e.Import(t.Context(), geo.ImportRequest{Path: rightOfWay()}); err == nil {
		t.Error("expected an error for a route with no branch")
	}
}

// Re-importing the same bounding box is a normal thing to do — a source
// updates, a fetch is retried after a timeout — and it must converge rather
// than accumulate. The source's own identity is the key, so the second import
// updates one circuit and replaces its geometry; a second circuit that looks
// exactly as real as the first is the failure this guards.
func TestReimportingTheSameLineUpdatesOneCircuit(t *testing.T) {
	e := engine(t)

	line := source.Line{
		ExternalID: "way/168061174",
		Source:     "openstreetmap",
		Path:       rightOfWay(),
		VoltageKV:  230,
		Owner:      "Oncor",
	}

	first, firstRoute, err := e.ImportLine(t.Context(), line)
	if err != nil {
		t.Fatalf("ImportLine: %v", err)
	}

	// The source corrected the voltage and the operator between imports.
	line.VoltageKV = 345
	line.Owner = "Sharyland Utilities"

	second, secondRoute, err := e.ImportLine(t.Context(), line)
	if err != nil {
		t.Fatalf("ImportLine again: %v", err)
	}

	if second.ID != first.ID {
		t.Errorf("circuit id %s on re-import, want %s: the same way became two circuits",
			second.ID, first.ID)
	}

	if second.VoltageKV != 345 || second.Operator != "Sharyland Utilities" {
		t.Errorf("re-import left %g kV / %q; the correction was dropped",
			second.VoltageKV, second.Operator)
	}

	circuits, err := e.Circuits(t.Context(), 0)
	if err != nil {
		t.Fatalf("Circuits: %v", err)
	}

	if len(circuits) != 1 {
		t.Errorf("%d circuits after two imports of one way, want 1", len(circuits))
	}

	// The route follows the circuit rather than being orphaned by a fresh one.
	if secondRoute.CarrierID != first.ID {
		t.Errorf("route carries %s, want the circuit %s", secondRoute.CarrierID, first.ID)
	}

	routes, err := e.Routes(t.Context(), 0)
	if err != nil {
		t.Fatalf("Routes: %v", err)
	}

	if len(routes) != 1 {
		t.Errorf("%d routes after two imports of one way, want 1 (was %s)",
			len(routes), firstRoute.ID)
	}
}

// A source that publishes structures produces a route whose spans start and
// end at them. Anything interpolated between two recorded structures would
// still be stored as Surveyed, which is the claim §3.3 says only this
// combination may make.
func TestImportedStructuresBecomeTheVerticesThemselves(t *testing.T) {
	e := engine(t)

	towers := rightOfWay()

	structures := make([]source.Structure, len(towers))
	for index, position := range towers {
		structures[index] = source.Structure{Position: position}
	}

	circuit, route, err := e.ImportLine(t.Context(), source.Line{
		ExternalID: "way/318827441",
		Source:     "openstreetmap",
		Path:       towers,
		Structures: structures,
	})
	if err != nil {
		t.Fatalf("ImportLine: %v", err)
	}

	if route.Provenance != asset.Surveyed {
		t.Errorf("provenance %q, want surveyed", route.Provenance)
	}

	if route.CarrierID != circuit.ID {
		t.Errorf("route carries %s, want circuit %s", route.CarrierID, circuit.ID)
	}

	spans, err := e.Spans(t.Context(), route.ID)
	if err != nil {
		t.Fatalf("Spans: %v", err)
	}

	if len(spans) != len(towers)-1 {
		t.Fatalf("%d spans for %d structures, want %d: something was interpolated between them",
			len(spans), len(towers), len(towers)-1)
	}

	for index, span := range spans {
		if span.Geometry[0] != towers[index] {
			t.Errorf("span %d starts at %v, want the structure at %v",
				index+1, span.Geometry[0], towers[index])
		}
	}
}

// Longitude is deliberately not monotonic in source order: the second
// structure sits west of the first and the third sits between them. A
// fixture whose source order and geographic order coincide cannot tell
// "ordinal is the loop index" apart from "ordinal is a geographic sort" —
// they would produce the same stored output either way. See import.go's
// Ordinal comment.
func TestImportRecordsWhatEachStructureIs(t *testing.T) {
	e, st := testEngine(t)
	ctx := context.Background()

	recorded := []source.Structure{
		{Position: shape.Point{Lon: -97.7958, Lat: 30.30}, Kind: "pole"},
		{Position: shape.Point{Lon: -97.8000, Lat: 30.31}, Kind: "tower", Material: "steel"},
		{Position: shape.Point{Lon: -97.7979, Lat: 30.29}, Kind: "tower"},
	}

	route, err := e.Import(ctx, geo.ImportRequest{
		CarrierID:  gid.NewCircuit(),
		Path:       shape.Line{{Lon: -97.80, Lat: 30.3}, {Lon: -97.7958, Lat: 30.3}},
		Structures: recorded,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	back, err := st.Structures().ForRoute(ctx, route.ID)
	if err != nil {
		t.Fatalf("ForRoute: %v", err)
	}

	if len(back) != 3 {
		t.Fatalf("stored %d structures, imported 3", len(back))
	}

	for index, structure := range back {
		if structure.Ordinal != index || structure.RouteID != route.ID {
			t.Fatalf("structure %d = %+v", index, structure)
		}

		// Ordinal has to preserve the source's own order rather than
		// re-derive one from position — this is what pins that, since the
		// fixture's longitude is not sorted.
		if structure.Location != recorded[index].Position {
			t.Errorf("structure %d is at %v, want the source's structure %d at %v",
				index, structure.Location, index, recorded[index].Position)
		}

		if structure.Kind != recorded[index].Kind || structure.Material != recorded[index].Material {
			t.Errorf("structure %d = %+v, want kind %q material %q",
				index, structure, recorded[index].Kind, recorded[index].Material)
		}
	}
}

// Pins finding 2 of the 2026-08-04 review: Ordinal is 0-based and
// Span.Sequence is 1-based, so a join between them is an offset, not an
// equality. The join the risk extension will make between a span and its
// bounding structures lands on the wrong end of the span if this drifts.
//
// Longitude is not monotonic in source order for the same reason as above —
// a fixture that happens to be geographically sorted could pass this even if
// Ordinal silently became a geographic sort instead of source order.
//
// Assertions compare actual stored geometry (span endpoints against
// structure locations), not index arithmetic restating the formula under
// test.
func TestSpanSequenceIsBoundedByAdjacentOrdinals(t *testing.T) {
	e, st := testEngine(t)
	ctx := context.Background()

	recorded := []source.Structure{
		{Position: shape.Point{Lon: -97.795, Lat: 30.30}, Kind: "tower"},
		{Position: shape.Point{Lon: -97.800, Lat: 30.31}, Kind: "tower"},
		{Position: shape.Point{Lon: -97.792, Lat: 30.29}, Kind: "pole"},
		{Position: shape.Point{Lon: -97.798, Lat: 30.32}, Kind: "tower"},
	}

	path := make(shape.Line, len(recorded))
	for index, structure := range recorded {
		path[index] = structure.Position
	}

	route, err := e.Import(ctx, geo.ImportRequest{
		CarrierID:  gid.NewCircuit(),
		Path:       path,
		Structures: recorded,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	spans, err := e.Spans(ctx, route.ID)
	if err != nil {
		t.Fatalf("Spans: %v", err)
	}

	structures, err := st.Structures().ForRoute(ctx, route.ID)
	if err != nil {
		t.Fatalf("ForRoute: %v", err)
	}

	byOrdinal := make(map[int]asset.Structure, len(structures))
	for _, structure := range structures {
		byOrdinal[structure.Ordinal] = structure
	}

	if len(spans) == 0 {
		t.Fatal("no spans to check")
	}

	for _, span := range spans {
		start, ok := byOrdinal[span.Sequence-1]
		if !ok {
			t.Fatalf("no structure at ordinal %d for span sequence %d", span.Sequence-1, span.Sequence)
		}

		end, ok := byOrdinal[span.Sequence]
		if !ok {
			t.Fatalf("no structure at ordinal %d for span sequence %d", span.Sequence, span.Sequence)
		}

		if span.Geometry[0] != start.Location {
			t.Errorf("span sequence %d starts at %v, want structure ordinal %d at %v",
				span.Sequence, span.Geometry[0], span.Sequence-1, start.Location)
		}

		if got := span.Geometry[len(span.Geometry)-1]; got != end.Location {
			t.Errorf("span sequence %d ends at %v, want structure ordinal %d at %v",
				span.Sequence, got, span.Sequence, end.Location)
		}
	}
}

// Proves finding 1 of the 2026-08-04 review is fixed: before it, a re-import
// left the previous route's structures filed under a route id nothing could
// look up again, because Import mints a fresh route id on every call and
// StructureStore.Put's own replace-not-append cleanup only ever sees that new,
// empty id.
//
// This goes through the real Import path — not Put called twice with a
// hand-supplied route id, which is what let the orphan hide through six
// reviews — and counts structures across the whole store rather than just
// under the new route, since the bug is invisible from the new route alone.
func TestReimportForgetsThePreviousRoutesStructures(t *testing.T) {
	e, st := testEngine(t)
	ctx := context.Background()

	carrier := gid.NewCircuit()

	first, err := e.Import(ctx, geo.ImportRequest{
		CarrierID:  carrier,
		Path:       shape.Line{{Lon: -97.80, Lat: 30.30}, {Lon: -97.79, Lat: 30.31}},
		Structures: asStructures(shape.Line{{Lon: -97.80, Lat: 30.30}, {Lon: -97.795, Lat: 30.305}, {Lon: -97.79, Lat: 30.31}}),
	})
	if err != nil {
		t.Fatalf("first Import: %v", err)
	}

	second, err := e.Import(ctx, geo.ImportRequest{
		CarrierID:  carrier,
		Path:       shape.Line{{Lon: -97.78, Lat: 30.40}, {Lon: -97.77, Lat: 30.41}},
		Structures: asStructures(shape.Line{{Lon: -97.78, Lat: 30.40}, {Lon: -97.77, Lat: 30.41}}),
	})
	if err != nil {
		t.Fatalf("second Import: %v", err)
	}

	if second.ID == first.ID {
		t.Fatalf("both imports produced route %s; Import always mints a fresh id, "+
			"so the test proves nothing if this ever changes", first.ID)
	}

	stale, err := st.Structures().ForRoute(ctx, first.ID)
	if err != nil {
		t.Fatalf("ForRoute(first): %v", err)
	}

	current, err := st.Structures().ForRoute(ctx, second.ID)
	if err != nil {
		t.Fatalf("ForRoute(second): %v", err)
	}

	// The total across the store, not just under the new route: reading only
	// len(current) would pass even with the bug, since the new route's own
	// structures were never in question.
	if total := len(stale) + len(current); total != 2 {
		t.Errorf("%d structures across the store after a re-import, want 2 "+
			"(%d stranded under the previous route %s, %d under the current one)",
			total, len(stale), first.ID, len(current))
	}
}

func TestASpanCountIsOneLessThanItsStructures(t *testing.T) {
	e, st := testEngine(t)
	ctx := context.Background()

	route, err := e.Import(ctx, geo.ImportRequest{
		CarrierID:  gid.NewCircuit(),
		Path:       shape.Line{{Lon: -97.80, Lat: 30.3}, {Lon: -97.7958, Lat: 30.3}},
		Structures: threeStructures(),
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	spans, err := e.Spans(ctx, route.ID)
	if err != nil {
		t.Fatalf("Spans: %v", err)
	}

	structures, err := st.Structures().ForRoute(ctx, route.ID)
	if err != nil {
		t.Fatalf("ForRoute: %v", err)
	}

	// The invariant that makes a route a line rather than a set of points.
	if len(spans) != len(structures)-1 {
		t.Fatalf("%d spans between %d structures", len(spans), len(structures))
	}
}
