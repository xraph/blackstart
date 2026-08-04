package geo_test

import (
	"testing"

	"github.com/xraph/blackstart/extensions/geo"
	"github.com/xraph/blackstart/extensions/geo/asset"
	"github.com/xraph/blackstart/extensions/geo/shape"
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
