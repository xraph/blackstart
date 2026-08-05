package shape_test

import (
	"testing"

	"github.com/xraph/blackstart/extensions/geo/shape"
)

func line(coords ...[2]float64) shape.Line {
	out := make(shape.Line, 0, len(coords))
	for _, c := range coords {
		out = append(out, shape.Point{Lon: c[0], Lat: c[1]})
	}

	return out
}

// pointInRing reports whether pt lies within ring, by ray casting. A test
// helper, not a package export: the only way this package can check what
// ground a Cover result actually claims.
func pointInRing(pt shape.Point, ring shape.Polygon) bool {
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

func anyPolygonContains(polys shape.MultiPolygon, pt shape.Point) bool {
	for _, p := range polys {
		if pointInRing(pt, p) {
			return true
		}
	}

	return false
}

// A corridor is closed, or it is not a polygon and nothing can fill it.
func TestCorridorClosesItsRing(t *testing.T) {
	t.Parallel()

	got, err := shape.Corridor(line([2]float64{-101, 34}, [2]float64{-100, 34}), 500)
	if err != nil {
		t.Fatalf("Corridor: %v", err)
	}

	if got[0] != got[len(got)-1] {
		t.Errorf("corridor is not closed: starts %v, ends %v", got[0], got[len(got)-1])
	}
}

// A wider buffer covers more ground. The area is the figure a reader takes
// from a blast radius, so it has to move with the input that sets it.
func TestCorridorWidensWithItsBuffer(t *testing.T) {
	t.Parallel()

	path := line([2]float64{-101, 34}, [2]float64{-100, 34})

	narrow, err := shape.Corridor(path, 100)
	if err != nil {
		t.Fatalf("Corridor: %v", err)
	}

	wide, err := shape.Corridor(path, 1000)
	if err != nil {
		t.Fatalf("Corridor: %v", err)
	}

	if shape.PolygonAreaM2(wide) <= shape.PolygonAreaM2(narrow) {
		t.Errorf("a 1000 m buffer covers %v m², a 100 m buffer %v m²",
			shape.PolygonAreaM2(wide), shape.PolygonAreaM2(narrow))
	}
}

// A path with fewer than two points has no direction and cannot be buffered.
func TestCorridorRefusesAPathThatIsNotOne(t *testing.T) {
	t.Parallel()

	if _, err := shape.Corridor(line([2]float64{-101, 34}), 500); err == nil {
		t.Error("Corridor accepted a one-point path")
	}
}

// THE assertion. Two failures forty kilometers apart are two extents, not one
// containing everything between them. A convex hull returns one polygon here
// and claims an area larger than the state.
func TestUnionKeepsDisjointCorridorsApart(t *testing.T) {
	t.Parallel()

	west, err := shape.Corridor(line([2]float64{-101, 34}, [2]float64{-100.9, 34}), 500)
	if err != nil {
		t.Fatalf("Corridor: %v", err)
	}

	east, err := shape.Corridor(line([2]float64{-97, 30}, [2]float64{-96.9, 30}), 500)
	if err != nil {
		t.Fatalf("Corridor: %v", err)
	}

	got := shape.Cover([]shape.Polygon{west, east})

	if len(got) != 2 {
		t.Errorf("Cover returned %d polygons for two distant corridors, want 2", len(got))
	}
}

// Two corridors that genuinely overlap are not merged: Cover does not
// attempt a real polygonal union, so both are returned exactly as given.
// The property that matters is honesty, not deduplication — every polygon
// in the result is one of the inputs, and nothing claims ground outside
// them.
func TestCoverKeepsOverlappingCorridorsSeparate(t *testing.T) {
	t.Parallel()

	path := line([2]float64{-101, 34}, [2]float64{-100.9, 34})

	first, err := shape.Corridor(path, 500)
	if err != nil {
		t.Fatalf("Corridor: %v", err)
	}

	second, err := shape.Corridor(path, 600)
	if err != nil {
		t.Fatalf("Corridor: %v", err)
	}

	got := shape.Cover([]shape.Polygon{first, second})

	if len(got) != 2 {
		t.Fatalf("Cover returned %d polygons for two overlapping corridors, want 2", len(got))
	}

	if got[0][0] != first[0] || got[1][0] != second[0] {
		t.Errorf("Cover did not return the two corridors as given: got %v, %v", got[0], got[1])
	}

	// A point well outside both corridors' 600 m half-width must not be
	// claimed by either returned polygon: ~2.2 km north of the shared path.
	outside := shape.Point{Lon: -101, Lat: 34.02}
	if anyPolygonContains(got, outside) {
		t.Errorf("Cover claims ground at %v, more than a kilometer from either corridor", outside)
	}
}

// The case that caught the convex-hull bug. Two corridors meeting in an L —
// one running west to east, the other turning south from its eastern end —
// leave a concave notch between them that was never within 500 m of either
// conductor. A hull over the pair fills that notch; Cover must not.
func TestCoverDoesNotFillTheConcaveNotch(t *testing.T) {
	t.Parallel()

	// ~92 km west to east along latitude 34.
	westEast, err := shape.Corridor(line([2]float64{-101, 34}, [2]float64{-100, 34}), 500)
	if err != nil {
		t.Fatalf("Corridor: %v", err)
	}

	// ~55 km south from the west-east corridor's eastern end.
	southward, err := shape.Corridor(line([2]float64{-100, 34}, [2]float64{-100, 33.505}), 500)
	if err != nil {
		t.Fatalf("Corridor: %v", err)
	}

	got := shape.Cover([]shape.Polygon{westEast, southward})

	// A point in the concave notch: west of the southward corridor, south of
	// the west-east corridor, well outside the 500 m buffer of either one,
	// but inside the convex hull a naive merge would have drawn around the L.
	notch := shape.Point{Lon: -100.3, Lat: 33.8}

	if anyPolygonContains(got, notch) {
		t.Errorf("Cover claims the concave notch at %v as affected ground; "+
			"it is more than 500 m from both conductors", notch)
	}
}

// A cascade that tripped nothing covered no ground. An empty extent, not a
// degenerate point at the origin, which would draw in the Gulf of Guinea.
func TestUnionOfNothingIsEmpty(t *testing.T) {
	t.Parallel()

	if got := shape.Cover(nil); len(got) != 0 {
		t.Errorf("Cover(nil) returned %d polygons, want 0", len(got))
	}
}
