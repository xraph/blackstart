// Package shape is the geometry blackstart stores, independent of PostGIS.
//
// Pure: no database, no context, no I/O. The geometry is written by a fabriq
// lifecycle hook inside the aggregate's own transaction, which makes it awkward
// to test through the database — so everything that can be wrong about a shape
// is decided here, where a test is a function call.
//
// Coordinates are longitude-first. That is what WKT means by (x y z) and what
// PostGIS reads, and it is the opposite of the (lat, lon) order every mapping
// API and every human uses. Getting it backwards puts a Texas substation in
// the Indian Ocean — far enough to be obvious, which is the only reason this
// mistake is ever caught.
package shape

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// SRID is the spatial reference every stored geometry uses.
//
// One, fixed. Mixed-SRID geometry is a family of bugs where a distance comes
// back in degrees and looks like a small number of meters.
const SRID = 4326

// EarthRadiusM is the mean radius used for great-circle distance.
//
// A sphere, not the WGS84 ellipsoid. The error is under 0.5% — smaller than
// the difference between a line's surveyed route and the straight-line
// approximation Phase 1 stores for it, so a more exact figure would be
// precision the input does not have.
const EarthRadiusM = 6371008.8

// Point is one position on the ellipsoid.
type Point struct {
	// Lon is degrees east, in the range [-180, 180].
	Lon float64 `json:"lon"`

	// Lat is degrees north, in the range [-90, 90].
	Lat float64 `json:"lat"`

	// Elevation is meters above the ellipsoid. Zero when unknown, which is
	// most of the time in Phase 1 — the viewer draws conductors above ground
	// and a structure height is what lifts them.
	Elevation float64 `json:"elevation"`
}

// Validate reports whether a point is on the earth.
//
// # Errors
//
// Returns an error naming the out-of-range ordinate. Latitude beyond ±90 is
// the signature of transposed coordinates, so the message says so — that is
// the mistake this check exists to catch.
func (p Point) Validate() error {
	if math.IsNaN(p.Lon) || math.IsNaN(p.Lat) {
		return fmt.Errorf("shape: point (%v, %v) is not a number", p.Lon, p.Lat)
	}

	if p.Lat < -90 || p.Lat > 90 {
		return fmt.Errorf(
			"shape: latitude %v is outside ±90; a longitude in the latitude "+
				"field is the usual cause, and coordinates here are (lon, lat)",
			p.Lat)
	}

	if p.Lon < -180 || p.Lon > 180 {
		return fmt.Errorf("shape: longitude %v is outside ±180", p.Lon)
	}

	return nil
}

// DistanceTo is the great-circle distance in meters, ignoring elevation.
//
// The haversine form rather than the spherical law of cosines, because the law
// of cosines loses all its precision at short distances — and a span between
// two adjacent transmission structures is a short distance.
func (p Point) DistanceTo(other Point) float64 {
	lat1, lat2 := radians(p.Lat), radians(other.Lat)
	dLat, dLon := radians(other.Lat-p.Lat), radians(other.Lon-p.Lon)

	sinLat, sinLon := math.Sin(dLat/2), math.Sin(dLon/2)
	a := sinLat*sinLat + math.Cos(lat1)*math.Cos(lat2)*sinLon*sinLon

	return 2 * EarthRadiusM * math.Asin(math.Min(1, math.Sqrt(a)))
}

// AzimuthTo is the initial bearing in degrees clockwise from true north.
//
// The initial bearing, not a constant one: a great circle's heading changes
// along its length, and for a span short enough to matter the difference is
// negligible while for a whole line it is not. Named to say which it is.
func (p Point) AzimuthTo(other Point) float64 {
	lat1, lat2 := radians(p.Lat), radians(other.Lat)
	dLon := radians(other.Lon - p.Lon)

	y := math.Sin(dLon) * math.Cos(lat2)
	x := math.Cos(lat1)*math.Sin(lat2) - math.Sin(lat1)*math.Cos(lat2)*math.Cos(dLon)

	degrees := math.Atan2(y, x) * 180 / math.Pi

	// Normalized into [0, 360) rather than left in atan2's ±180, because a
	// bearing is conventionally reported that way and a negative one reads as
	// an error.
	return math.Mod(degrees+360, 360)
}

// Line is an ordered path.
type Line []Point

// Validate reports whether a line can be stored.
func (l Line) Validate() error {
	if len(l) < 2 {
		return fmt.Errorf(
			"shape: a line needs two points; got %d. A one-point line has no "+
				"length and no direction, and PostGIS refuses it too", len(l))
	}

	for index, point := range l {
		if err := point.Validate(); err != nil {
			return fmt.Errorf("shape: point %d: %w", index, err)
		}
	}

	return nil
}

// LengthM is the summed great-circle length in meters.
func (l Line) LengthM() float64 {
	var total float64

	for index := 1; index < len(l); index++ {
		total += l[index-1].DistanceTo(l[index])
	}

	return total
}

// EWKT renders the line as PostGIS extended well-known text.
//
// Always three ordinates, even when every elevation is zero. PostGIS decides a
// geometry's dimensionality from its first coordinate, so a line that dropped
// the zeros would be 2D and would fail to insert into a geometry(LineStringZ)
// column — at write time, in a lifecycle hook, inside somebody else's
// transaction.
func (l Line) EWKT() string {
	var out strings.Builder

	out.WriteString("SRID=" + strconv.Itoa(SRID) + ";LINESTRING(")

	for index, point := range l {
		if index > 0 {
			out.WriteString(",")
		}

		out.WriteString(point.ordinates())
	}

	out.WriteString(")")

	return out.String()
}

// EWKT renders the point as PostGIS extended well-known text.
func (p Point) EWKT() string {
	return "SRID=" + strconv.Itoa(SRID) + ";POINT(" + p.ordinates() + ")"
}

// ordinates renders one coordinate triple.
//
// 'g' with full precision rather than a fixed number of decimal places: a
// rounded coordinate is a moved one, and at these latitudes the sixth decimal
// place is about a tenth of a meter.
func (p Point) ordinates() string {
	return format(p.Lon) + " " + format(p.Lat) + " " + format(p.Elevation)
}

func format(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

func radians(degrees float64) float64 { return degrees * math.Pi / 180 }
