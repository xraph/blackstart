package geo_test

import (
	"errors"
	"math"
	"testing"

	"github.com/xraph/blackstart/extensions/geo"
	"github.com/xraph/blackstart/extensions/geo/asset"
	"github.com/xraph/blackstart/extensions/geo/shape"
	"github.com/xraph/blackstart/extensions/geo/store/memory"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

var (
	comanche = shape.Point{Lon: -97.7856, Lat: 32.2983}
	dallas   = shape.Point{Lon: -96.7970, Lat: 32.7767}
)

func engine(t *testing.T) *geo.Engine {
	t.Helper()

	e, err := geo.NewEngine(memory.New(), 0, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	return e
}

func TestTracingABranchStoresItsSpans(t *testing.T) {
	e := engine(t)
	branch := id.MustNew("nmdl")

	route, err := e.Trace(t.Context(), geo.TraceRequest{
		BranchID: branch, From: comanche, To: dallas,
	})
	if err != nil {
		t.Fatalf("Trace: %v", err)
	}

	// Interpolated, and the row says so. A per-span probability conditioned on
	// a straight-line guess is a different claim from one conditioned on a
	// surveyed route.
	if route.Provenance != asset.Interpolated {
		t.Errorf("provenance is %q", route.Provenance)
	}

	spans, err := e.Spans(t.Context(), route.ID)
	if err != nil {
		t.Fatalf("Spans: %v", err)
	}

	// About 107 km at the 400 m default is roughly 267 spans. The point is
	// that it is many, not one — one span per line is the modelling error
	// per-span geometry exists to avoid.
	if len(spans) < 200 {
		t.Fatalf("a 107 km line came back as %d spans", len(spans))
	}

	// The stored route length is what the spans add up to. A report should not
	// depend on whether the geometry happened to be loaded, so it is stored —
	// which means it can disagree, and this is the check that it does not.
	var summed float64
	for _, span := range spans {
		summed += span.LengthM
	}

	if math.Abs(summed-route.LengthM) > 1 {
		t.Errorf("the route says %v m and its spans total %v m", route.LengthM, summed)
	}
}

// Spans are numbered from one and come back in order, because a span's
// position is what relates it to its neighbors and to the weather at that
// point.
func TestSpansComeBackInOrderNumberedFromOne(t *testing.T) {
	e := engine(t)

	route, err := e.Trace(t.Context(), geo.TraceRequest{
		BranchID: id.MustNew("nmdl"), From: comanche, To: dallas, SpanM: 20_000,
	})
	if err != nil {
		t.Fatalf("Trace: %v", err)
	}

	spans, err := e.Spans(t.Context(), route.ID)
	if err != nil {
		t.Fatalf("Spans: %v", err)
	}

	for index, span := range spans {
		if span.Sequence != index+1 {
			t.Fatalf("span %d has sequence %d", index, span.Sequence)
		}
	}

	// And consecutive spans share a structure, so the route is continuous
	// rather than a set of disconnected segments.
	for index := 1; index < len(spans); index++ {
		previous := spans[index-1].Geometry
		if previous[len(previous)-1] != spans[index].Geometry[0] {
			t.Errorf("spans %d and %d do not meet", index, index+1)
		}
	}
}

// Azimuth is what a wind loading resolves against, so a span running east must
// report an easterly bearing rather than whatever the last computation left.
func TestEachSpanCarriesItsOwnBearing(t *testing.T) {
	e := engine(t)

	// A line running due north, where every span's bearing is zero, and one
	// running due east, where every span's is ninety. Two directions rather
	// than one, because a stuck value would pass either test alone.
	for name, ends := range map[string][2]shape.Point{
		"north": {{Lon: -97, Lat: 32}, {Lon: -97, Lat: 33}},
		"east":  {{Lon: -97, Lat: 0}, {Lon: -96, Lat: 0}},
	} {
		t.Run(name, func(t *testing.T) {
			route, err := e.Trace(t.Context(), geo.TraceRequest{
				BranchID: id.MustNew("nmdl"), From: ends[0], To: ends[1], SpanM: 20_000,
			})
			if err != nil {
				t.Fatalf("Trace: %v", err)
			}

			spans, err := e.Spans(t.Context(), route.ID)
			if err != nil {
				t.Fatalf("Spans: %v", err)
			}

			want := 0.0
			if name == "east" {
				want = 90
			}

			for _, span := range spans {
				if math.Abs(span.AzimuthDeg-want) > 0.5 {
					t.Fatalf("a %s-running span bears %v°", name, span.AzimuthDeg)
				}
			}
		})
	}
}

// A branch has one route, and that is how everything downstream finds it.
func TestARouteIsFoundByTheBranchItCarries(t *testing.T) {
	e := engine(t)
	branch := id.MustNew("nmdl")

	route, err := e.Trace(t.Context(), geo.TraceRequest{
		BranchID: branch, From: comanche, To: dallas,
	})
	if err != nil {
		t.Fatalf("Trace: %v", err)
	}

	found, err := e.RouteForBranch(t.Context(), branch)
	if err != nil {
		t.Fatalf("RouteForBranch: %v", err)
	}

	if found.ID != route.ID {
		t.Errorf("got route %s, want %s", found.ID, route.ID)
	}

	if _, err := e.RouteForBranch(t.Context(), id.MustNew("nmdl")); !errors.Is(err, bserr.ErrNotFound) {
		t.Errorf("an untraced branch returned %v, want ErrNotFound", err)
	}
}

// A path over the ground that carries nothing is not part of the network, and
// storing one would leave geometry nothing can ever find.
func TestARouteMustNameItsBranch(t *testing.T) {
	e := engine(t)

	_, err := e.Trace(t.Context(), geo.TraceRequest{From: comanche, To: dallas})
	if err == nil {
		t.Error("a route with no branch was stored")
	}
}

// Coordinates that are not on the earth are refused at the boundary rather
// than stored and found later by PostGIS.
func TestEndpointsOffTheEarthAreRefused(t *testing.T) {
	e := engine(t)

	_, err := e.Trace(t.Context(), geo.TraceRequest{
		BranchID: id.MustNew("nmdl"),
		From:     shape.Point{Lon: 32.2983, Lat: -97.7856},
		To:       dallas,
	})
	if err == nil {
		t.Error("a transposed coordinate was accepted")
	}
}
