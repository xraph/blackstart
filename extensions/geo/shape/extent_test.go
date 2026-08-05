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

	got := shape.Union([]shape.Polygon{west, east})

	if len(got) != 2 {
		t.Errorf("Union returned %d polygons for two distant corridors, want 2", len(got))
	}
}

// Two corridors that genuinely overlap are one piece of affected ground, and
// reporting them separately would double-count the area.
func TestUnionMergesOverlappingCorridors(t *testing.T) {
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

	if got := shape.Union([]shape.Polygon{first, second}); len(got) != 1 {
		t.Errorf("Union returned %d polygons for two overlapping corridors, want 1", len(got))
	}
}

// A cascade that tripped nothing covered no ground. An empty extent, not a
// degenerate point at the origin, which would draw in the Gulf of Guinea.
func TestUnionOfNothingIsEmpty(t *testing.T) {
	t.Parallel()

	if got := shape.Union(nil); len(got) != 0 {
		t.Errorf("Union(nil) returned %d polygons, want 0", len(got))
	}
}
