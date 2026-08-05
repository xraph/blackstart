package shape

import (
	"fmt"
	"math"

	"github.com/xraph/blackstart/pkg/bserr"
)

// Polygon is a single closed ring: the first point repeated last. No holes —
// a blast radius is ground near failed conductors, never ground with a hole
// cut out of it.
type Polygon []Point

// MultiPolygon is zero or more polygons. Nothing here guarantees they are
// disjoint or even distinct pieces of ground — see Cover, presently the
// only producer of a MultiPolygon in this package, for why.
type MultiPolygon []Polygon

// Corridor is the ground within halfWidthM of a path, on both sides.
//
// Each vertex is offset perpendicular to the path's local bearing —
// outbound at +90° from the bearing, then, walking the path backwards, at
// -90° — and the ring is closed by repeating the first point. At an
// interior vertex the local bearing is the circular mean of the bearing
// arriving and the bearing leaving, so the corridor turns with the path
// instead of leaving a gap or an overlap at the bend. The ends are square,
// not rounded.
//
// This is the shape a blast radius is built from, one path at a time. What
// several corridors add up to is Cover's job, not this one's.
//
// # Errors
//
// Wraps [bserr.ErrInvalid] when the path has fewer than two points (no
// direction to offset from) or the half-width is not positive.
func Corridor(l Line, halfWidthM float64) (Polygon, error) {
	if len(l) < 2 {
		return nil, fmt.Errorf(
			"%w: a corridor needs a path of at least two points; got %d",
			bserr.ErrInvalid, len(l))
	}

	if halfWidthM <= 0 || math.IsNaN(halfWidthM) {
		return nil, fmt.Errorf(
			"%w: corridor half-width %v m must be positive", bserr.ErrInvalid, halfWidthM)
	}

	bearings := make([]float64, len(l))
	for i := range l {
		switch {
		case i == 0:
			bearings[i] = l[0].AzimuthTo(l[1])
		case i == len(l)-1:
			bearings[i] = l[i-1].AzimuthTo(l[i])
		default:
			bearings[i] = meanBearing(l[i-1].AzimuthTo(l[i]), l[i].AzimuthTo(l[i+1]))
		}
	}

	ring := make(Polygon, 0, 2*len(l)+1)
	for i, p := range l {
		ring = append(ring, destinationPoint(p, bearings[i]+90, halfWidthM))
	}

	for i := len(l) - 1; i >= 0; i-- {
		ring = append(ring, destinationPoint(l[i], bearings[i]-90, halfWidthM))
	}

	return append(ring, ring[0]), nil
}

// PolygonAreaM2 is the polygon's area in square meters: the shoelace formula
// on an equirectangular projection local to the polygon's own centroid.
//
// Not a geodesic area — an approximation, adequate for comparing two extents
// (a wider corridor covers more; a merged extent covers at least as much as
// its parts) and wrong to quote as the exact area of anything, especially
// anything large enough for the earth's curvature to matter over its span.
func PolygonAreaM2(p Polygon) float64 {
	if len(p) < 3 {
		return 0
	}

	var centerLon, centerLat float64
	for _, pt := range p {
		centerLon += pt.Lon
		centerLat += pt.Lat
	}

	centerLon /= float64(len(p))
	centerLat /= float64(len(p))
	centerLatRad := radians(centerLat)

	type xy struct{ x, y float64 }
	local := make([]xy, len(p))
	for i, pt := range p {
		local[i] = xy{
			x: radians(pt.Lon-centerLon) * math.Cos(centerLatRad) * EarthRadiusM,
			y: radians(pt.Lat-centerLat) * EarthRadiusM,
		}
	}

	var sum float64
	for i := range local {
		j := (i + 1) % len(local)
		sum += local[i].x*local[j].y - local[j].x*local[i].y
	}

	return math.Abs(sum) / 2
}

// Cover is every conductor's corridor, unmerged: one polygon per input, in
// the same order, and nothing else.
//
// An earlier version of this function attempted to merge overlapping
// corridors into one polygon, using a convex hull to stand in for a true
// polygonal union. Measured against two real corridors meeting at a right
// angle — the ordinary case for a branch that turns, not a rare one — that
// hull filled the entire concave notch between them with ground that was
// never within any conductor's buffer: about 18x the sum of the two
// corridors' own areas. A hull over conductors that turn is exactly the
// mistake Corridor's package doc rejects for a whole cascade, just scoped
// down to two conductors. Scoped down, it was still false.
//
// So there is no merge, and the guarantee is smaller but honest: every point
// inside some returned polygon is within halfWidthM of some conductor, and
// no point outside every returned polygon is claimed to be affected. What
// this does NOT guarantee is a partition. Where two input corridors overlap,
// both are returned whole, so summing PolygonAreaM2 across the result counts
// that overlap twice — it gives total corridor area, not distinct ground
// covered. A caller that needs the latter cannot get it by summing; that
// would require a real union, which this package does not attempt.
func Cover(in []Polygon) MultiPolygon {
	if len(in) == 0 {
		return nil
	}

	out := make(MultiPolygon, 0, len(in))

	return append(out, in...)
}

// destinationPoint is the point reached by travelling distanceM meters from
// p along the initial bearing bearingDeg (degrees clockwise from north): the
// direct geodesic problem, on the same sphere DistanceTo and AzimuthTo use,
// and the inverse of what they compute. Corridor is the only caller.
func destinationPoint(p Point, bearingDeg, distanceM float64) Point {
	lat1 := radians(p.Lat)
	lon1 := radians(p.Lon)
	brng := radians(bearingDeg)
	angular := distanceM / EarthRadiusM

	lat2 := math.Asin(math.Sin(lat1)*math.Cos(angular) + math.Cos(lat1)*math.Sin(angular)*math.Cos(brng))
	lon2 := lon1 + math.Atan2(
		math.Sin(brng)*math.Sin(angular)*math.Cos(lat1),
		math.Cos(angular)-math.Sin(lat1)*math.Sin(lat2),
	)

	return Point{
		// Wrapped into [-180, 180) rather than left to wander past the
		// antimeridian, so a corridor near it stays a valid point.
		Lon:       math.Mod(lon2*180/math.Pi+540, 360) - 180,
		Lat:       lat2 * 180 / math.Pi,
		Elevation: p.Elevation,
	}
}

// meanBearing is the circular mean of two bearings in degrees. Not (a+b)/2:
// bearings of 350° and 10° average to 0°, not 180°, so they're averaged as
// unit vectors and converted back, which handles the wrap correctly.
func meanBearing(a, b float64) float64 {
	ar, br := radians(a), radians(b)
	x := (math.Cos(ar) + math.Cos(br)) / 2
	y := (math.Sin(ar) + math.Sin(br)) / 2

	if x == 0 && y == 0 {
		// Exactly opposite bearings: no meaningful average. Falling back to
		// one of them is arbitrary but stable, and a 180° turn in a
		// conductor path is already a degenerate input.
		return a
	}

	return math.Mod(math.Atan2(y, x)*180/math.Pi+360, 360)
}
