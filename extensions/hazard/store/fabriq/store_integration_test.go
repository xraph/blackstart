//go:build integration

package fabriq_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xraph/fabriq"

	dsfabriq "github.com/xraph/blackstart/extensions/datasource/store/fabriq"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/hazard/conductor"
	hid "github.com/xraph/blackstart/extensions/hazard/id"
	hzstore "github.com/xraph/blackstart/extensions/hazard/store"
	hzfabriq "github.com/xraph/blackstart/extensions/hazard/store/fabriq"
	wxfabriq "github.com/xraph/blackstart/extensions/weather/store/fabriq"
	"github.com/xraph/blackstart/internal/entities"
	"github.com/xraph/blackstart/internal/testsupport"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

const tenantID = "blackstart"

var issuance = time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

// harness brings up Postgres, runs the migration groups through one
// orchestrator, provisions the restricted application role, and opens Fabriq.
//
// The weather and datasource groups come along because a computation's
// cycle_id edge targets weather_cycle, whose own feed_id edge targets
// datasource_feed — and the registry refuses an edge whose target it has never
// heard of.
func harness(t *testing.T) *hzfabriq.Store {
	t.Helper()

	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, dsfabriq.Group(), wxfabriq.Group(), hzfabriq.Group())

	// The application connects as a non-superuser, because PostgreSQL row
	// security never constrains a superuser — a test that connected as one
	// would pass every isolation check while proving nothing.
	appDSN := testsupport.AppDSN(t, superDSN)

	reg, err := entities.Build(
		dsfabriq.Contributor{}, wxfabriq.Contributor{}, hzfabriq.Contributor{})
	if err != nil {
		t.Fatalf("building the registry: %v", err)
	}

	fab, _, err := fabriq.Open(context.Background(), reg, fabriq.Config{
		Postgres: fabriq.PostgresConfig{DSN: appDSN},
	})
	if err != nil {
		t.Fatalf("opening fabriq: %v", err)
	}

	t.Cleanup(func() { _ = fab.Close() })

	s, err := hzfabriq.New(fab, tenantID)
	if err != nil {
		t.Fatalf("building the store: %v", err)
	}

	return s
}

func anAssignment(branch id.ID) conductor.Assignment {
	return conductor.Assignment{
		ID:            hid.NewAssignment(),
		BranchID:      branch,
		ConductorCode: "drake",
		Basis:         conductor.Surveyed,
		Note:          "surveyed 2026-07",
	}
}

func TestAssignmentRoundTrip(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	branch := id.MustNew("brch")
	want := anAssignment(branch)

	if err := s.Assignments().Put(ctx, want); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Assignments().Get(ctx, want.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got != want {
		t.Errorf("read back %+v, wrote %+v", got, want)
	}

	byBranch, err := s.Assignments().ForBranch(ctx, branch)
	if err != nil {
		t.Fatalf("ForBranch: %v", err)
	}

	if byBranch.ID != want.ID {
		t.Errorf("ForBranch gave %s, wrote %s", byBranch.ID, want.ID)
	}
}

// The unique index is the real constraint, and this is what proves it exists
// rather than merely being in the migration text. The store's own read-then-
// delete would hide it, so the second row is written straight through the
// index by giving both assignments the same branch.
func TestOneConductorPerBranch(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	branch := id.MustNew("brch")

	assumed := anAssignment(branch)
	assumed.ConductorCode = "hawk"
	assumed.Basis = conductor.AssumedByVoltage

	if err := s.Assignments().Put(ctx, assumed); err != nil {
		t.Fatalf("Put: %v", err)
	}

	surveyed := anAssignment(branch)

	if err := s.Assignments().Put(ctx, surveyed); err != nil {
		t.Fatalf("second Put: %v", err)
	}

	got, err := s.Assignments().ForBranch(ctx, branch)
	if err != nil {
		t.Fatalf("ForBranch: %v", err)
	}

	if got.ID != surveyed.ID || got.ConductorCode != "drake" {
		t.Errorf("ForBranch gave %+v, want the surveyed one", got)
	}

	if _, err := s.Assignments().Get(ctx, assumed.ID); !errors.Is(err, bserr.ErrNotFound) {
		t.Errorf("the replaced assignment is still readable: %v", err)
	}

	all, err := s.Assignments().List(ctx, pkgstore.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(all) != 1 {
		t.Errorf("listed %d assignments for one branch", len(all))
	}
}

func TestUnassignedBranchIsNotFound(t *testing.T) {
	s := harness(t)

	_, err := s.Assignments().ForBranch(context.Background(), id.MustNew("brch"))
	if !errors.Is(err, bserr.ErrNotFound) {
		t.Fatalf("ForBranch error = %v, want bserr.ErrNotFound", err)
	}
}

func TestAssignmentListPagesForward(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	for range 5 {
		if err := s.Assignments().Put(ctx, anAssignment(id.MustNew("brch"))); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	first, err := s.Assignments().List(ctx, pkgstore.Page{Limit: 2})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(first) != 2 {
		t.Fatalf("first page has %d rows, want 2", len(first))
	}

	next, err := s.Assignments().List(ctx, pkgstore.Page{Limit: 2, After: first[1].ID})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	for _, got := range next {
		if got.ID.String() <= first[1].ID.String() {
			t.Errorf("the cursor returned %s, which is not after %s", got.ID, first[1].ID)
		}
	}
}

func aComputation(cycle id.ID, at time.Time) hzstore.Computation {
	return hzstore.Computation{
		ID:         hid.NewComputation(),
		CycleID:    cycle,
		RunID:      id.MustNew("prvrun"),
		ComputedAt: at,
		Spans:      12,
		Unrated:    3,
		Values:     60,
	}
}

func TestComputationRoundTrip(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	want := aComputation(id.MustNew("wxcyc"), issuance)

	if err := s.Computations().Create(ctx, want); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Computations().Get(ctx, want.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got != want {
		t.Errorf("read back %+v, wrote %+v", got, want)
	}
}

// A pass with no provenance run still stores. The column defaults to the empty
// string and the model carries no notnull tag for it, and this is the test that
// would fail if either changed.
func TestAComputationWithoutARunStores(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	want := aComputation(id.MustNew("wxcyc"), issuance)
	want.RunID = id.Nil
	want.Spans = 0
	want.Unrated = 0
	want.Values = 0

	if err := s.Computations().Create(ctx, want); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Computations().Get(ctx, want.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got != want {
		t.Errorf("read back %+v, wrote %+v", got, want)
	}
}

// Several passes over one cycle is the normal case, one per correction to the
// assignments, and every one is kept.
func TestACycleKeepsEveryPass(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	cycle := id.MustNew("wxcyc")
	first := aComputation(cycle, issuance)
	second := aComputation(cycle, issuance.Add(time.Hour))

	for _, in := range []hzstore.Computation{first, second} {
		if err := s.Computations().Create(ctx, in); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	got, err := s.Computations().ForCycle(ctx, cycle)
	if err != nil {
		t.Fatalf("ForCycle: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("ForCycle gave %d passes, want 2", len(got))
	}

	if got[0].ID != second.ID {
		t.Errorf("ForCycle led with %s, want the newest pass %s", got[0].ID, second.ID)
	}
}

func TestValuesRoundTripThroughTheHypertable(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	span := id.MustNew(gid.Span)

	want := make([]hzstore.SpanValue, 0, len(hzstore.Quantities())*3)

	for _, quantity := range hzstore.Quantities() {
		for hour := range 3 {
			want = append(want, hzstore.SpanValue{
				SpanID:   span,
				Quantity: quantity,
				IssuedAt: issuance,
				ValidAt:  issuance.Add(time.Duration(hour) * time.Hour),
				Value:    float64(hour + 1),
			})
		}
	}

	if err := s.Values().Write(ctx, want); err != nil {
		t.Fatalf("Write: %v", err)
	}

	keys := make([]string, 0, len(hzstore.Quantities()))
	for _, quantity := range hzstore.Quantities() {
		keys = append(keys, hzstore.ValueKey(span, quantity, issuance))
	}

	got, err := s.Values().Read(ctx, keys, issuance, issuance.Add(6*time.Hour))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("read %d values, wrote %d", len(got), len(want))
	}
}

// Half-open, so a caller walking a series hour by hour does not read every
// boundary twice. Checked against the live table rather than only in memory,
// because the predicate that implements it is hand-written SQL.
func TestTheHypertableReadWindowIsHalfOpen(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	span := id.MustNew(gid.Span)

	if err := s.Values().Write(ctx, []hzstore.SpanValue{
		{SpanID: span, Quantity: hzstore.QuantityAmpacity, IssuedAt: issuance, ValidAt: issuance, Value: 1},
		{SpanID: span, Quantity: hzstore.QuantityAmpacity, IssuedAt: issuance, ValidAt: issuance.Add(time.Hour), Value: 2},
		{SpanID: span, Quantity: hzstore.QuantityAmpacity, IssuedAt: issuance, ValidAt: issuance.Add(2 * time.Hour), Value: 3},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	key := hzstore.ValueKey(span, hzstore.QuantityAmpacity, issuance)

	got, err := s.Values().Read(ctx, []string{key}, issuance, issuance.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("read %d values over a two-hour half-open window, want 2", len(got))
	}
}

// An empty window selects nothing, and saying so beats returning nothing
// silently — a caller that swapped from and to would otherwise see an empty
// result and conclude the pass never ran.
func TestAnInvertedWindowIsRefused(t *testing.T) {
	s := harness(t)

	span := id.MustNew(gid.Span)
	key := hzstore.ValueKey(span, hzstore.QuantityAmpacity, issuance)

	_, err := s.Values().Read(
		context.Background(), []string{key}, issuance.Add(time.Hour), issuance)
	if err == nil {
		t.Fatal("a window ending before it began was accepted")
	}
}
