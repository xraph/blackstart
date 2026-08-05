package memory_test

import (
	"context"
	"testing"

	"github.com/xraph/blackstart/extensions/geo/asset"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/geo/shape"
	"github.com/xraph/blackstart/extensions/geo/store/memory"
	"github.com/xraph/blackstart/pkg/id"
)

// structuresFor builds n structures on a route, dense ordinals a few hundred
// meters apart along a line running east.
func structuresFor(route id.ID, n int) []asset.Structure {
	out := make([]asset.Structure, 0, n)

	for i := range n {
		out = append(out, asset.Structure{
			ID:      gid.NewStructure(),
			RouteID: route,
			Ordinal: i,
			Location: shape.Point{
				Lon: -97.80 + float64(i)*0.003,
				Lat: 30.3,
			},
			Kind: "tower",
		})
	}

	return out
}

func TestStructuresComeBackInOrder(t *testing.T) {
	st := memory.New()
	ctx := context.Background()
	route := gid.NewRoute()

	// Written out of order on purpose: ordinal is the order, not insertion.
	in := []asset.Structure{
		{
			ID: gid.NewStructure(), RouteID: route, Ordinal: 2,
			Location: shape.Point{Lon: -97.79, Lat: 30.3}, Kind: "pole",
		},
		{
			ID: gid.NewStructure(), RouteID: route, Ordinal: 0,
			Location: shape.Point{Lon: -97.80, Lat: 30.3}, Kind: "tower",
		},
		{
			ID: gid.NewStructure(), RouteID: route, Ordinal: 1,
			Location: shape.Point{Lon: -97.795, Lat: 30.3}, Kind: "tower",
		},
	}

	if err := st.Structures().Put(ctx, route, in); err != nil {
		t.Fatalf("Put: %v", err)
	}

	back, err := st.Structures().ForRoute(ctx, route)
	if err != nil {
		t.Fatalf("ForRoute: %v", err)
	}

	for index, structure := range back {
		if structure.Ordinal != index {
			t.Fatalf("position %d has ordinal %d; ForRoute must order by it",
				index, structure.Ordinal)
		}
	}
}

func TestPutReplacesTheWholeRoute(t *testing.T) {
	st := memory.New()
	ctx := context.Background()
	route := gid.NewRoute()

	long := structuresFor(route, 5)
	if err := st.Structures().Put(ctx, route, long); err != nil {
		t.Fatalf("first Put: %v", err)
	}

	if err := st.Structures().Put(ctx, route, long[:2]); err != nil {
		t.Fatalf("second Put: %v", err)
	}

	back, err := st.Structures().ForRoute(ctx, route)
	if err != nil {
		t.Fatalf("ForRoute: %v", err)
	}

	if len(back) != 2 {
		t.Fatalf("read %d structures after a re-survey, want 2; a route that "+
			"keeps its old structures has two paths and no way to say which "+
			"is current", len(back))
	}
}

func TestAStructureOnAnotherRouteIsRejected(t *testing.T) {
	st := memory.New()
	route := gid.NewRoute()

	wrong := structuresFor(gid.NewRoute(), 1)

	if err := st.Structures().Put(context.Background(), route, wrong); err == nil {
		t.Fatal("a structure belonging to another route was stored under this " +
			"one; the route it names and the route it is filed under have to " +
			"agree or neither means anything")
	}
}
