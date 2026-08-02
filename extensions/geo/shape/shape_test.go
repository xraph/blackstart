package shape_test

import (
	"math"
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/geo/shape"
)

// Two real substations, far enough apart that a wrong formula is obvious and
// close enough to be a plausible transmission line. Comanche Peak to Dallas,
// roughly.
var (
	comanche = shape.Point{Lon: -97.7856, Lat: 32.2983, Elevation: 240}
	dallas   = shape.Point{Lon: -96.7970, Lat: 32.7767, Elevation: 130}
)

// Checked against the equirectangular approximation rather than against this
// implementation's own output: 0.4784° of latitude is 53.2 km, and 0.9886° of
// longitude at 32.5° north is 92.8 km, so the hypotenuse is 107.0 km. A
// tolerance of 1 km because the reference is itself an approximation — a
// tighter bound would assert agreement with a rounding rather than with
// geography.
func TestDistanceIsInMetersNotDegrees(t *testing.T) {
	got := comanche.DistanceTo(dallas)

	if math.Abs(got-107_000) > 1_000 {
		t.Errorf("distance is %v m, want about 107 km", got)
	}

	// And it is symmetric, which a sign error in the haversine would break.
	if back := dallas.DistanceTo(comanche); math.Abs(got-back) > 1e-6 {
		t.Errorf("distance is not symmetric: %v then %v", got, back)
	}

	// A point is no distance from itself. The haversine's arcsine is where a
	// domain error would show up, as NaN rather than zero.
	if same := comanche.DistanceTo(comanche); same != 0 {
		t.Errorf("a point is %v m from itself", same)
	}
}

// The mistake this package exists to catch. Latitude beyond ±90 is the
// signature of transposed coordinates, and the message has to say so —
// otherwise the reader sees a range check and not a diagnosis.
func TestTransposedCoordinatesAreRefusedByName(t *testing.T) {
	transposed := shape.Point{Lon: 32.2983, Lat: -97.7856}

	err := transposed.Validate()
	if err == nil {
		t.Fatal("a latitude of -97.8 was accepted")
	}

	if !strings.Contains(err.Error(), "lon, lat") {
		t.Errorf("the message does not name the likely cause: %v", err)
	}
}

// Bearing is clockwise from north and reported in [0, 360). Dallas is east and
// slightly north of Comanche Peak, so the bearing is a little north of due
// east.
func TestAzimuthIsClockwiseFromNorth(t *testing.T) {
	got := comanche.AzimuthTo(dallas)

	if got < 45 || got > 90 {
		t.Errorf("azimuth is %v°, want between north-east and due east", got)
	}

	// Due north, exactly, which a swapped atan2 would report as due east.
	north := shape.Point{Lon: -97, Lat: 33}.AzimuthTo(shape.Point{Lon: -97, Lat: 34})
	if math.Abs(north) > 1e-9 {
		t.Errorf("due north reported as %v°", north)
	}

	// And due south comes back as 180 rather than -180.
	south := shape.Point{Lon: -97, Lat: 34}.AzimuthTo(shape.Point{Lon: -97, Lat: 33})
	if math.Abs(south-180) > 1e-9 {
		t.Errorf("due south reported as %v°", south)
	}
}

// PostGIS decides a geometry's dimensionality from its first coordinate, so a
// line that dropped zero elevations would be 2D and would fail to insert into
// a LineStringZ column — at write time, inside a lifecycle hook, in somebody
// else's transaction.
func TestEWKTAlwaysCarriesThreeOrdinates(t *testing.T) {
	flat := shape.Line{{Lon: -97, Lat: 32}, {Lon: -96, Lat: 33}}

	got := flat.EWKT()

	if !strings.HasPrefix(got, "SRID=4326;LINESTRING(") {
		t.Fatalf("EWKT is %q", got)
	}

	first, _, _ := strings.Cut(strings.TrimPrefix(got, "SRID=4326;LINESTRING("), ",")
	if len(strings.Fields(first)) != 3 {
		t.Errorf("the first coordinate is %q, which is not three ordinates", first)
	}

	// Longitude first, which is what WKT means by (x y z) and the opposite of
	// what every mapping API and every human uses.
	if !strings.HasPrefix(first, "-97 32") {
		t.Errorf("coordinate order is wrong: %q", first)
	}
}

// Linear interpolation of coordinates is correct at the equator and wrong
// everywhere else, so a test built around the equator would pass on a broken
// implementation. This one runs at 60° north, where the two disagree by enough
// to see.
func TestInterpolationFollowsTheGreatCircleAtHighLatitude(t *testing.T) {
	west := shape.Point{Lon: -10, Lat: 60}
	east := shape.Point{Lon: 10, Lat: 60}

	middle := west.Interpolate(east, 0.5)

	// By symmetry the midpoint sits on the prime meridian.
	if math.Abs(middle.Lon) > 1e-9 {
		t.Errorf("the midpoint is at longitude %v, want 0", middle.Lon)
	}

	// And north of both ends: a great circle between two points at the same
	// latitude bulges toward the pole. Linear interpolation would return
	// exactly 60.
	if middle.Lat <= 60 {
		t.Errorf("the midpoint is at latitude %v, which is not on the great circle",
			middle.Lat)
	}

	// Both halves are the same length, which a mis-scaled interpolation would
	// break while still landing on the meridian.
	if first, second := west.DistanceTo(middle), middle.DistanceTo(east); math.Abs(first-second) > 1e-6 {
		t.Errorf("the halves are %v and %v m", first, second)
	}
}

func TestInterpolatingBetweenIdenticalPointsIsNotNaN(t *testing.T) {
	got := comanche.Interpolate(comanche, 0.5)

	if math.IsNaN(got.Lat) || math.IsNaN(got.Lon) {
		t.Errorf("got %v", got)
	}
}

// Ends are the stated points, not interpolations, so a route ends exactly
// where the substation is.
func TestASyntheticRouteKeepsItsStatedEnds(t *testing.T) {
	route, err := shape.SyntheticRoute(comanche, dallas, 400)
	if err != nil {
		t.Fatalf("SyntheticRoute: %v", err)
	}

	if route[0] != comanche || route[len(route)-1] != dallas {
		t.Errorf("the route runs %v to %v", route[0], route[len(route)-1])
	}

	// About 107 km at a 400 m nominal span is roughly 267 spans.
	if spans := len(route) - 1; spans < 255 || spans > 280 {
		t.Errorf("got %d spans, want about 267", spans)
	}

	// And the summed span lengths reproduce the direct distance, which a
	// mis-scaled interpolation would not.
	if got, want := route.LengthM(), comanche.DistanceTo(dallas); math.Abs(got-want) > 1 {
		t.Errorf("the route is %v m long but its ends are %v m apart", got, want)
	}
}

// A line shorter than one span is two points. Rounding up to a single nominal
// span would move one end.
func TestAShortRouteIsNotStretchedToOneNominalSpan(t *testing.T) {
	near := shape.Point{Lon: -97.7856, Lat: 32.2993}

	route, err := shape.SyntheticRoute(comanche, near, 400)
	if err != nil {
		t.Fatalf("SyntheticRoute: %v", err)
	}

	if len(route) != 2 {
		t.Errorf("a %v m route came back with %d points",
			comanche.DistanceTo(near), len(route))
	}
}

func TestASpanLengthThatCannotDivideARouteIsRefused(t *testing.T) {
	for _, nominal := range []float64{0, -1, math.NaN()} {
		if _, err := shape.SyntheticRoute(comanche, dallas, nominal); err == nil {
			t.Errorf("a nominal span of %v was accepted", nominal)
		}
	}
}

// Each span is a separately failing thing — a conductor breaks between two
// structures, not along a whole line — so the split has to preserve every
// point and share the joins.
func TestSpansShareTheirEndPoints(t *testing.T) {
	route := shape.Line{
		{Lon: -97, Lat: 32}, {Lon: -96.5, Lat: 32.2}, {Lon: -96, Lat: 32.4},
	}

	spans := route.Spans()

	if len(spans) != 2 {
		t.Fatalf("got %d spans from three points", len(spans))
	}

	if spans[0][1] != spans[1][0] {
		t.Error("adjacent spans do not share a structure")
	}

	if got, want := spans[0].LengthM()+spans[1].LengthM(), route.LengthM(); math.Abs(got-want) > 1e-6 {
		t.Errorf("the spans total %v m but the route is %v m", got, want)
	}
}

func TestALineNeedsTwoPoints(t *testing.T) {
	if err := (shape.Line{{Lon: -97, Lat: 32}}).Validate(); err == nil {
		t.Error("a one-point line was accepted")
	}
}
