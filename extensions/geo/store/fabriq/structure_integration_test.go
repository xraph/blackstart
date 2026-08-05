//go:build integration

package fabriq_test

import (
	"context"
	"testing"

	"github.com/xraph/fabriq"

	"github.com/xraph/blackstart/extensions/geo"
	"github.com/xraph/blackstart/extensions/geo/asset"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/geo/shape"
	"github.com/xraph/blackstart/extensions/geo/source"
	geofabriq "github.com/xraph/blackstart/extensions/geo/store/fabriq"
	"github.com/xraph/blackstart/internal/entities"
	"github.com/xraph/blackstart/internal/testsupport"
	"github.com/xraph/blackstart/pkg/id"
)

const tenantID = "blackstart"

// spanMetersDeg is 200 meters expressed as a fraction of a degree of
// latitude — the survey's measured median span, so structuresFor lays its
// points out the way a real route does rather than clustering them at one
// coordinate.
const spanMetersDeg = 200.0 / 111320.0

// liveStore brings up Postgres, runs the geo migration group together with
// Fabriq's own, provisions the restricted application role, and opens a geo
// store over it.
//
// The application connects as a non-superuser, because PostgreSQL row
// security never constrains a superuser — a test that connected as one would
// pass every isolation check while proving nothing.
func liveStore(t *testing.T) (*geofabriq.Store, func()) {
	t.Helper()

	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, geofabriq.Group())

	appDSN := testsupport.AppDSN(t, superDSN)

	reg, err := entities.Build(geofabriq.Contributor{})
	if err != nil {
		t.Fatalf("building the registry: %v", err)
	}

	fab, _, err := fabriq.Open(context.Background(), reg, fabriq.Config{
		Postgres: fabriq.PostgresConfig{DSN: appDSN},
	})
	if err != nil {
		t.Fatalf("opening fabriq: %v", err)
	}

	st, err := geofabriq.New(fab, tenantID)
	if err != nil {
		t.Fatalf("building the store: %v", err)
	}

	return st, func() { _ = fab.Close() }
}

// tenantContext is the context every store call runs under. Tenancy itself is
// stamped by fabriqstore.Base from the tenant liveStore built the store with,
// not from anything carried on the context — so this is a plain background
// context, named for what the test is scoped to rather than for a mechanism
// it does not use.
func tenantContext(t *testing.T) context.Context {
	t.Helper()

	return context.Background()
}

// structuresFor builds n structures on route with dense ordinals about 200 m
// apart — the survey's measured median span — the first a steel tower.
//
// Returned in the reverse of ordinal order. Slice order, Ordinal and Latitude
// all increasing together was the bug in this fixture: it meant deleting
// `ORDER BY ordinal ASC` from ForRoute's query broke nothing, because a fresh
// table with no updates returns rows in close to their insertion order
// anyway. Reversing the slice decouples insertion order from Ordinal, so a
// missing ORDER BY now produces visibly wrong output instead of coincidentally
// correct output.
func structuresFor(t *testing.T, route id.ID, n int) []asset.Structure {
	t.Helper()

	out := make([]asset.Structure, n)

	for i := range n {
		out[n-1-i] = asset.Structure{
			ID:      gid.NewStructure(),
			RouteID: route,
			Ordinal: i,
			Location: shape.Point{
				Lon: -100.0,
				Lat: 35.0 + float64(i)*spanMetersDeg,
			},
			HeightM:  30,
			Kind:     "tower",
			Material: "steel",
		}
	}

	return out
}

// sourceStructuresFor is structuresFor's counterpart for the real Import
// path, which takes source.Structure rather than an already-identified
// asset.Structure. Kept in source order (not reversed, unlike structuresFor)
// because Import — not this fixture — is what assigns Ordinal from that
// order, and the point of the tests using this is to exercise that
// assignment rather than route around it.
func sourceStructuresFor(n int) []source.Structure {
	out := make([]source.Structure, n)

	for i := range n {
		out[i] = source.Structure{
			Position: shape.Point{
				Lon: -100.0,
				Lat: 35.0 + float64(i)*spanMetersDeg,
			},
			Kind:     "tower",
			Material: "steel",
		}
	}

	return out
}

// lineFor is sourceStructuresFor's positions alone, which Import.Path must
// validate even though the structures determine the stored geometry when
// they are supplied.
func lineFor(structures []source.Structure) shape.Line {
	out := make(shape.Line, len(structures))

	for index, structure := range structures {
		out[index] = structure.Position
	}

	return out
}

func TestStructuresRoundTripThroughPostgres(t *testing.T) {
	st, done := liveStore(t)
	defer done()

	ctx := tenantContext(t)
	route := gid.NewRoute()
	in := structuresFor(t, route, 400)

	if err := st.Structures().Put(ctx, route, in); err != nil {
		t.Fatalf("Put: %v", err)
	}

	back, err := st.Structures().ForRoute(ctx, route)
	if err != nil {
		t.Fatalf("ForRoute: %v", err)
	}

	if len(back) != 400 {
		t.Fatalf("read %d structures, wrote 400", len(back))
	}

	for index, structure := range back {
		if structure.Ordinal != index {
			t.Fatalf("position %d has ordinal %d; order is what makes "+
				"consecutive structures a span", index, structure.Ordinal)
		}
	}

	if back[0].Kind != "tower" || back[0].Material != "steel" {
		t.Errorf("class did not survive the round trip: %+v", back[0])
	}
}

func TestAnUnclassedStructureRoundTripsAsUnclassed(t *testing.T) {
	st, done := liveStore(t)
	defer done()

	ctx := tenantContext(t)
	route := gid.NewRoute()

	bare := structuresFor(t, route, 1)
	bare[0].Kind = ""
	bare[0].Material = ""

	if err := st.Structures().Put(ctx, route, bare); err != nil {
		t.Fatalf("Put: %v", err)
	}

	back, err := st.Structures().ForRoute(ctx, route)
	if err != nil {
		t.Fatalf("ForRoute: %v", err)
	}

	// Empty has to come back empty rather than as a default, because risk
	// distinguishes a structure it cannot curve from one it can.
	if back[0].Kind != "" || back[0].Material != "" {
		t.Errorf("an unclassed structure came back as %+v", back[0])
	}
}

// Put's contract on RouteStore.Put says a member whose own route disagrees
// with the passed route is refused rather than silently reassigned. The
// structure store makes the identical promise, and this is what would fail if
// the guard were ever dropped: the structure would land on someone else's
// route instead of erroring.
func TestPutRefusesAStructureFromAnotherRoute(t *testing.T) {
	st, done := liveStore(t)
	defer done()

	ctx := tenantContext(t)
	route := gid.NewRoute()
	stray := structuresFor(t, gid.NewRoute(), 1)

	if err := st.Structures().Put(ctx, route, stray); err == nil {
		t.Fatal("Put accepted a structure whose route disagreed with the one passed")
	}
}

// Put replaces a route's whole set rather than appending, so a re-import
// leaves exactly the new structures behind rather than the old ones beside
// them with nothing to say which is current.
//
// This calls Put directly rather than a fixed route id supplied by the test:
// a hand-supplied route id shared across two Put calls is a condition the
// real re-import path never meets — Import mints a fresh route id every
// time — and testing only that condition is exactly what let finding 1 of the
// 2026-08-04 review hide through six prior reviews. TestReimportReplacesTheRealImportPath
// below covers the path this test cannot.
func TestPutReplacesRatherThanAppends(t *testing.T) {
	st, done := liveStore(t)
	defer done()

	ctx := tenantContext(t)
	route := gid.NewRoute()

	first := structuresFor(t, route, 5)
	if err := st.Structures().Put(ctx, route, first); err != nil {
		t.Fatalf("first Put: %v", err)
	}

	second := structuresFor(t, route, 3)
	if err := st.Structures().Put(ctx, route, second); err != nil {
		t.Fatalf("second Put: %v", err)
	}

	back, err := st.Structures().ForRoute(ctx, route)
	if err != nil {
		t.Fatalf("ForRoute: %v", err)
	}

	if len(back) != 3 {
		t.Fatalf("read %d structures after a replacing Put, want 3 — the "+
			"first set's rows were still there", len(back))
	}
}

// Proves finding 1 of the 2026-08-04 review is fixed, against the live
// database rather than the memory store: before it, a re-import left the
// previous route's structures filed under a route id nothing could look up
// again, because Import mints a fresh route id on every call and
// StructureStore.Put's own replace-not-append cleanup only ever sees that
// new, empty id.
//
// This goes through the real Engine.Import path over the Fabriq store — not
// Put called twice with a hand-supplied route id, which is
// TestPutReplacesRatherThanAppends above and is exactly what let the orphan
// hide — and counts structures across the whole store rather than just under
// the new route, since the bug is invisible from the new route alone.
func TestReimportReplacesTheRealImportPath(t *testing.T) {
	st, done := liveStore(t)
	defer done()

	ctx := tenantContext(t)

	e, err := geo.NewEngine(st, 0, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	carrier := gid.NewCircuit()

	firstStructures := sourceStructuresFor(5)

	first, err := e.Import(ctx, geo.ImportRequest{
		CarrierID:  carrier,
		Path:       lineFor(firstStructures),
		Structures: firstStructures,
	})
	if err != nil {
		t.Fatalf("first Import: %v", err)
	}

	secondStructures := sourceStructuresFor(3)

	second, err := e.Import(ctx, geo.ImportRequest{
		CarrierID:  carrier,
		Path:       lineFor(secondStructures),
		Structures: secondStructures,
	})
	if err != nil {
		t.Fatalf("second Import: %v", err)
	}

	if second.ID == first.ID {
		t.Fatalf("both imports produced route %s; Import always mints a fresh "+
			"id, so this test proves nothing if that ever changes", first.ID)
	}

	stale, err := st.Structures().ForRoute(ctx, first.ID)
	if err != nil {
		t.Fatalf("ForRoute(first): %v", err)
	}

	current, err := st.Structures().ForRoute(ctx, second.ID)
	if err != nil {
		t.Fatalf("ForRoute(second): %v", err)
	}

	// The total across the store, not just under the new route: reading only
	// len(current) would pass even with the bug, since the new route's own
	// structures were never in question.
	if total := len(stale) + len(current); total != 3 {
		t.Errorf("%d structures across the store after a re-import, want 3 "+
			"(%d stranded under the previous route %s, %d under the current one)",
			total, len(stale), first.ID, len(current))
	}
}
