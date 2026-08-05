package shape_test

import (
	"math"
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/geo/shape"
)

// The property the stored row depends on: what EWKT writes, ParseLineEWKT
// reads back unchanged. The geometry column is a rebuildable index over this
// text, so a lossy round trip would make a reindex quietly move the network.
func TestEWKTRoundTripsExactly(t *testing.T) {
	route, err := shape.SyntheticRoute(comanche, dallas, 5_000)
	if err != nil {
		t.Fatalf("SyntheticRoute: %v", err)
	}

	back, err := shape.ParseLineEWKT(route.EWKT())
	if err != nil {
		t.Fatalf("ParseLineEWKT: %v", err)
	}

	if len(back) != len(route) {
		t.Fatalf("wrote %d points and read %d", len(route), len(back))
	}

	for index := range route {
		// Exactly, not within a tolerance: 'g' with full precision is
		// round-trippable for float64, and a tolerance here would hide the
		// day someone switched it to a fixed number of decimal places.
		if back[index] != route[index] {
			t.Errorf("point %d went out as %v and came back as %v",
				index, route[index], back[index])
		}
	}

	if got, want := back.LengthM(), route.LengthM(); math.Abs(got-want) > 1e-9 {
		t.Errorf("length went out as %v and came back as %v", want, got)
	}
}

// A parser that silently accepted a POLYGON as a line would produce a route
// through the vertices of a shape, which is a plausible-looking network that
// nothing would flag.
func TestOnlyLineStringsAreRead(t *testing.T) {
	for name, text := range map[string]string{
		"a point":        "SRID=4326;POINT(-97 32 0)",
		"a polygon":      "SRID=4326;POLYGON((-97 32 0,-96 32 0,-96 33 0,-97 32 0))",
		"another SRID":   "SRID=3857;LINESTRING(-97 32 0,-96 33 0)",
		"unbalanced":     "SRID=4326;LINESTRING(-97 32 0,-96 33 0",
		"one point":      "SRID=4326;LINESTRING(-97 32 0)",
		"not a number":   "SRID=4326;LINESTRING(-97 north 0,-96 33 0)",
		"too many parts": "SRID=4326;LINESTRING(-97 32 0 1,-96 33 0)",
		"transposed":     "SRID=4326;LINESTRING(32 -97 0,33 -96 0)",
		"empty":          "",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := shape.ParseLineEWKT(text); err == nil {
				t.Errorf("%s was accepted", name)
			}
		})
	}
}

// A geometry with no SRID prefix is ambiguous rather than wrong — PostGIS
// treats it as unknown — so it reads, and two ordinates read with elevation
// unknown rather than being refused.
func TestGeometryFromElsewhereStillReads(t *testing.T) {
	got, err := shape.ParseLineEWKT("LINESTRING(-97 32, -96 33)")
	if err != nil {
		t.Fatalf("ParseLineEWKT: %v", err)
	}

	if len(got) != 2 || got[0].Elevation != 0 {
		t.Errorf("got %v", got)
	}
}

// A stored geometry that cannot be read is a row somebody has to look at, and
// a vague message turns that into a hunt.
func TestTheErrorSaysWhatItFoundInstead(t *testing.T) {
	_, err := shape.ParseLineEWKT("SRID=4326;POINT(-97 32 0)")
	if err == nil {
		t.Fatal("a point was accepted as a line")
	}

	if !strings.Contains(err.Error(), "POINT") {
		t.Errorf("the message does not name what it found: %v", err)
	}
}

// The property a structure's row depends on: what Point.EWKT writes,
// ParsePointEWKT reads back unchanged.
func TestPointEWKTRoundTripsExactly(t *testing.T) {
	back, err := shape.ParsePointEWKT(comanche.EWKT())
	if err != nil {
		t.Fatalf("ParsePointEWKT: %v", err)
	}

	if back != comanche {
		t.Errorf("point went out as %v and came back as %v", comanche, back)
	}
}

// A well-formed point, a malformed one, and a LINESTRING handed to it by
// mistake. The LINESTRING case is the one that matters: a parser that fell
// back to "take the first vertex" would accept a two-point line as a point
// and silently discard the second coordinate.
func TestOnlyPointsAreRead(t *testing.T) {
	for name, text := range map[string]string{
		"a linestring":    "SRID=4326;LINESTRING(-97 32 0,-96 33 0)",
		"a polygon":       "SRID=4326;POLYGON((-97 32 0,-96 32 0,-96 33 0,-97 32 0))",
		"another SRID":    "SRID=3857;POINT(-97 32 0)",
		"unbalanced":      "SRID=4326;POINT(-97 32 0",
		"not a number":    "SRID=4326;POINT(-97 north 0)",
		"too many parts":  "SRID=4326;POINT(-97 32 0 1)",
		"transposed":      "SRID=4326;POINT(32 -97 0)",
		"two coordinates": "SRID=4326;POINT(-97 32 0,-96 33 0)",
		"empty":           "",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := shape.ParsePointEWKT(text); err == nil {
				t.Errorf("%s was accepted", name)
			}
		})
	}
}

// A well-formed point with no SRID prefix reads with elevation defaulting to
// zero when omitted, for ParseLineEWKT's reason.
func TestPointFromElsewhereStillReads(t *testing.T) {
	got, err := shape.ParsePointEWKT("POINT(-97 32)")
	if err != nil {
		t.Fatalf("ParsePointEWKT: %v", err)
	}

	if got.Lon != -97 || got.Lat != 32 || got.Elevation != 0 {
		t.Errorf("got %v", got)
	}
}

// A stored geometry that cannot be read as a point is a row somebody has to
// look at, and a vague message turns that into a hunt.
func TestThePointErrorSaysWhatItFoundInstead(t *testing.T) {
	_, err := shape.ParsePointEWKT("SRID=4326;LINESTRING(-97 32 0,-96 33 0)")
	if err == nil {
		t.Fatal("a linestring was accepted as a point")
	}

	if !strings.Contains(err.Error(), "LINESTRING") {
		t.Errorf("the message does not name what it found: %v", err)
	}
}
