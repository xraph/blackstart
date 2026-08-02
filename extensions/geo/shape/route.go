package shape

import (
	"fmt"
	"math"
)

// Interpolate returns the point a fraction of the way along the great circle
// from p to other.
//
// Spherical interpolation rather than linear interpolation of the coordinates.
// Linear is correct at the equator and wrong everywhere else — at 60° north it
// puts the midpoint of an east-west line tens of meters off the actual path,
// and the error grows with latitude, so it looks fine in a test built around
// the equator and wrong in Alaska.
func (p Point) Interpolate(other Point, fraction float64) Point {
	lat1, lon1 := radians(p.Lat), radians(p.Lon)
	lat2, lon2 := radians(other.Lat), radians(other.Lon)

	// The angular distance between the two points. Zero when they coincide,
	// which would divide by zero below.
	angle := p.DistanceTo(other) / EarthRadiusM
	if angle < 1e-12 {
		return p
	}

	a := math.Sin((1-fraction)*angle) / math.Sin(angle)
	b := math.Sin(fraction*angle) / math.Sin(angle)

	x := a*math.Cos(lat1)*math.Cos(lon1) + b*math.Cos(lat2)*math.Cos(lon2)
	y := a*math.Cos(lat1)*math.Sin(lon1) + b*math.Cos(lat2)*math.Sin(lon2)
	z := a*math.Sin(lat1) + b*math.Sin(lat2)

	return Point{
		Lon:       math.Atan2(y, x) * 180 / math.Pi,
		Lat:       math.Atan2(z, math.Hypot(x, y)) * 180 / math.Pi,
		Elevation: p.Elevation + fraction*(other.Elevation-p.Elevation),
	}
}

// SyntheticRoute is a straight great-circle path between two points, broken at
// a nominal span length.
//
// Synthetic, and named so at every use. A real transmission line follows a
// surveyed right-of-way that bends around terrain and property, and its
// structures stand where the survey put them. This is a straight line with
// evenly spaced imaginary structures, present to prove the per-span data model
// carries what it claims — a per-span failure probability needs a per-span
// geometry to condition on, and there is no public per-structure dataset in
// Phase 1. HIFLD and OSM routes replace it without the model changing.
//
// # Errors
//
// Returns an error when either end is off the earth or the nominal span length
// is not positive.
func SyntheticRoute(from, to Point, nominalSpanM float64) (Line, error) {
	if err := from.Validate(); err != nil {
		return nil, err
	}

	if err := to.Validate(); err != nil {
		return nil, err
	}

	if nominalSpanM <= 0 || math.IsNaN(nominalSpanM) {
		return nil, fmt.Errorf(
			"shape: nominal span length %v m; a route cannot be divided by a "+
				"length that is not positive", nominalSpanM)
	}

	total := from.DistanceTo(to)

	// Two points and nothing between them when the whole line is shorter than
	// one span. Rounding up to a single span would move one end.
	if total <= nominalSpanM {
		return Line{from, to}, nil
	}

	// Rounded rather than truncated, so the actual spans stay closest to the
	// nominal length instead of all being up to twice it.
	count := max(int(math.Round(total/nominalSpanM)), 1)

	out := make(Line, 0, count+1)
	out = append(out, from)

	for step := 1; step < count; step++ {
		out = append(out, from.Interpolate(to, float64(step)/float64(count)))
	}

	// The stated end point rather than an interpolation at fraction 1, so the
	// route ends exactly where the substation is rather than within a rounding
	// error of it.
	out = append(out, to)

	return out, nil
}

// Spans breaks a line into its individual segments.
//
// Each span is a separately failing thing: a conductor breaks between two
// structures, not along a whole line, and weather at one end of a 200 km line
// says little about the other. That is why geometry is stored per span rather
// than one polyline per branch.
func (l Line) Spans() []Line {
	if len(l) < 2 {
		return nil
	}

	out := make([]Line, 0, len(l)-1)

	for index := 1; index < len(l); index++ {
		out = append(out, Line{l[index-1], l[index]})
	}

	return out
}
