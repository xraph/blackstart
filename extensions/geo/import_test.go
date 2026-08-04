package geo_test

import (
	"testing"

	"github.com/xraph/blackstart/extensions/geo"
	"github.com/xraph/blackstart/extensions/geo/asset"
	"github.com/xraph/blackstart/extensions/geo/shape"
	"github.com/xraph/blackstart/extensions/geo/source"
	"github.com/xraph/blackstart/pkg/id"
)

// A digitised right-of-way with a real corner in it.
func rightOfWay() shape.Line {
	return shape.Line{
		{Lon: -101.76, Lat: 34.54},
		{Lon: -102.10, Lat: 34.20},
		{Lon: -102.38, Lat: 34.20},
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
		Structures: rightOfWay(),
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
		Structures: structures,
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
func TestImportKeepsTheDigitisedCorner(t *testing.T) {
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

	circuit, route, err := e.ImportLine(t.Context(), source.Line{
		ExternalID: "way/318827441",
		Source:     "openstreetmap",
		Path:       towers,
		Structures: towers,
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
