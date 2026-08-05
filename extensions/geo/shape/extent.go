package shape

import (
	"fmt"
	"math"
	"sort"

	"github.com/xraph/blackstart/pkg/bserr"
)

// Polygon is a single closed ring: the first point repeated last. No holes —
// a blast radius is ground near failed conductors, never ground with a hole
// cut out of it.
type Polygon []Point

// MultiPolygon is zero or more disjoint polygons. More than one element
// means more than one piece of affected ground with nothing claimed to be
// affected in between — the distinction Union exists to preserve.
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
// several corridors add up to is Union's job, not this one's.
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

// Union groups polygons by whether they actually overlap and merges each
// group, leaving polygons that don't touch any other as separate entries.
//
// Not a convex hull. A hull drawn around the result would claim as affected
// every point between two groups that never overlapped — the exact mistake
// a blast radius cannot make, because area is the figure a reader takes from
// it. Two corridors forty kilometers apart stay two polygons here.
//
// Overlap is decided per pair by a bounding-box intersection (cheap
// pre-filter) then a real test on the rings: do any two edges cross, or does
// either ring contain a vertex of the other (the case edges alone miss when
// one ring sits entirely inside the other, as happens when the same path is
// buffered at two widths). Grouping is by connectivity — A can pull in C
// through B even if A and C don't overlap directly.
//
// A group that does overlap is not merged into a true polygonal union —
// exact, with shared edges and possible holes, is more geometry than this
// task carries and still would not be exact on a sphere. It is represented
// instead by the convex hull of its members' vertices. That over-states the
// group's own area somewhat, but never reaches into a group that did not
// overlap it, which is what the tests — and a blast radius — depend on.
func Union(in []Polygon) MultiPolygon {
	if len(in) == 0 {
		return nil
	}

	parent := make([]int, len(in))
	for i := range parent {
		parent[i] = i
	}

	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}

	join := func(a, b int) {
		if ra, rb := find(a), find(b); ra != rb {
			parent[ra] = rb
		}
	}

	boxes := make([][4]float64, len(in))
	for i, p := range in {
		boxes[i] = boundingBox(p)
	}

	for i := range in {
		for j := i + 1; j < len(in); j++ {
			if bboxesOverlap(boxes[i], boxes[j]) && polygonsOverlap(in[i], in[j]) {
				join(i, j)
			}
		}
	}

	groups := make(map[int][]int, len(in))
	order := make([]int, 0, len(in))

	for i := range in {
		root := find(i)
		if _, seen := groups[root]; !seen {
			order = append(order, root)
		}

		groups[root] = append(groups[root], i)
	}

	out := make(MultiPolygon, 0, len(order))
	for _, root := range order {
		members := groups[root]
		if len(members) == 1 {
			out = append(out, in[members[0]])
			continue
		}

		var pts []Point
		for _, idx := range members {
			pts = append(pts, in[idx]...)
		}

		out = append(out, convexHull(pts))
	}

	return out
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

// boundingBox is the polygon's axis-aligned extent in (minLon, minLat,
// maxLon, maxLat) order.
func boundingBox(p Polygon) [4]float64 {
	minLon, minLat := math.Inf(1), math.Inf(1)
	maxLon, maxLat := math.Inf(-1), math.Inf(-1)

	for _, pt := range p {
		minLon = math.Min(minLon, pt.Lon)
		maxLon = math.Max(maxLon, pt.Lon)
		minLat = math.Min(minLat, pt.Lat)
		maxLat = math.Max(maxLat, pt.Lat)
	}

	return [4]float64{minLon, minLat, maxLon, maxLat}
}

func bboxesOverlap(a, b [4]float64) bool {
	return a[0] <= b[2] && b[0] <= a[2] && a[1] <= b[3] && b[1] <= a[3]
}

// polygonsOverlap is the real test bboxesOverlap only pre-filters for: do
// the two rings share ground, either by crossing or by one containing the
// other (which edges alone miss — see Union's doc comment).
func polygonsOverlap(a, b Polygon) bool {
	for i := 0; i+1 < len(a); i++ {
		for j := 0; j+1 < len(b); j++ {
			if segmentsIntersect(a[i], a[i+1], b[j], b[j+1]) {
				return true
			}
		}
	}

	return (len(a) > 0 && pointInPolygon(a[0], b)) || (len(b) > 0 && pointInPolygon(b[0], a))
}

// segmentsIntersect reports whether segments p1q1 and p2q2 cross or touch,
// by the standard orientation test.
func segmentsIntersect(p1, q1, p2, q2 Point) bool {
	o1, o2 := orientation(p1, q1, p2), orientation(p1, q1, q2)
	o3, o4 := orientation(p2, q2, p1), orientation(p2, q2, q1)

	if o1 != o2 && o3 != o4 {
		return true
	}

	return (o1 == 0 && onSegment(p1, p2, q1)) ||
		(o2 == 0 && onSegment(p1, q2, q1)) ||
		(o3 == 0 && onSegment(p2, p1, q2)) ||
		(o4 == 0 && onSegment(p2, q1, q2))
}

// orientation is the turn from p to q to r: 0 colinear, 1 clockwise,
// 2 counterclockwise. Lon/Lat stand in for plane coordinates, used here only
// to decide topology, not distance.
func orientation(p, q, r Point) int {
	val := (q.Lat-p.Lat)*(r.Lon-q.Lon) - (q.Lon-p.Lon)*(r.Lat-q.Lat)

	switch {
	case val > 0:
		return 1
	case val < 0:
		return 2
	default:
		return 0
	}
}

// onSegment reports whether q, known colinear with p and r, lies within the
// bounding box of segment pr.
func onSegment(p, q, r Point) bool {
	return q.Lon <= math.Max(p.Lon, r.Lon) && q.Lon >= math.Min(p.Lon, r.Lon) &&
		q.Lat <= math.Max(p.Lat, r.Lat) && q.Lat >= math.Min(p.Lat, r.Lat)
}

// pointInPolygon reports whether pt lies within the ring, by ray casting.
func pointInPolygon(pt Point, ring Polygon) bool {
	inside := false

	for i, j := 0, len(ring)-1; i < len(ring); j, i = i, i+1 {
		pi, pj := ring[i], ring[j]
		if (pi.Lat > pt.Lat) != (pj.Lat > pt.Lat) {
			atX := (pj.Lon-pi.Lon)*(pt.Lat-pi.Lat)/(pj.Lat-pi.Lat) + pi.Lon
			if pt.Lon < atX {
				inside = !inside
			}
		}
	}

	return inside
}

// convexHull is the smallest convex ring containing every point, by Andrew's
// monotone chain. points is never empty: Union's only caller passes the
// concatenated vertices of two or more already-nonempty polygons. Used only
// to represent a group Union has determined overlaps — see its doc comment
// for why that is honest and a hull over the whole result would not be.
func convexHull(points []Point) Polygon {
	pts := append([]Point(nil), points...)
	sort.Slice(pts, func(i, j int) bool {
		if pts[i].Lon != pts[j].Lon {
			return pts[i].Lon < pts[j].Lon
		}

		return pts[i].Lat < pts[j].Lat
	})

	cross := func(o, a, b Point) float64 {
		return (a.Lon-o.Lon)*(b.Lat-o.Lat) - (a.Lat-o.Lat)*(b.Lon-o.Lon)
	}

	lower := make([]Point, 0, len(pts))
	for _, p := range pts {
		for len(lower) >= 2 && cross(lower[len(lower)-2], lower[len(lower)-1], p) <= 0 {
			lower = lower[:len(lower)-1]
		}
		lower = append(lower, p)
	}

	upper := make([]Point, 0, len(pts))
	for i := len(pts) - 1; i >= 0; i-- {
		p := pts[i]
		for len(upper) >= 2 && cross(upper[len(upper)-2], upper[len(upper)-1], p) <= 0 {
			upper = upper[:len(upper)-1]
		}
		upper = append(upper, p)
	}

	hull := make([]Point, 0, len(lower)+len(upper))
	hull = append(hull, lower[:len(lower)-1]...)
	hull = append(hull, upper[:len(upper)-1]...)

	if len(hull) == 0 {
		return nil
	}

	return append(Polygon(hull), hull[0])
}
