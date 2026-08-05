//go:build integration

package fabriq_test

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/fabriq"
	"github.com/xraph/grove/drivers/pgdriver"

	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/risk/store"
	riskfabriq "github.com/xraph/blackstart/extensions/risk/store/fabriq"
	"github.com/xraph/blackstart/internal/entities"
	"github.com/xraph/blackstart/internal/testsupport"
	"github.com/xraph/blackstart/pkg/id"
)

const tenantID = "blackstart"

var issuance = time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

// harness brings up Postgres, runs the risk migration group, provisions the
// restricted application role, and opens Fabriq bound to tenantID.
//
// The connection is the non-superuser fabriq_app role, not the migrating
// superuser: PostgreSQL row-level security never constrains a superuser, so
// a harness that read and wrote as one would pass every test in this file
// whether or not the value table's policy does anything at all.
func harness(t *testing.T) *riskfabriq.Store {
	t.Helper()

	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, riskfabriq.Group())

	appDSN := testsupport.AppDSN(t, superDSN)

	reg, err := entities.Build(riskfabriq.Contributor{})
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

	s, err := riskfabriq.New(fab, tenantID)
	if err != nil {
		t.Fatalf("building the store: %v", err)
	}

	return s
}

// harnessWithoutCoveringIndex is harness, plus one thing: it disables the
// planner's index and bitmap scan paths for fabriq_app's future sessions
// before Fabriq opens its connection pool.
//
// Why this exists: blackstart_risk_value_idx is (tenant_id, component_id,
// quantity, issued_at, valid_at), and ForComponent's WHERE clause pins every
// column ahead of valid_at to an equality. That makes a plain Index Scan on
// this index the planner's obvious choice for this query shape — verified
// with EXPLAIN (ANALYZE, BUFFERS) against this exact query — and a B-tree
// index scan visits its leaf entries in key order, so it hands back rows
// already sorted by valid_at regardless of physical insertion order and
// regardless of whether values.go's SQL text has an ORDER BY at all. A
// fixture that inserts out of order (reversed, below) is not enough by
// itself to make TestValuesRoundTripAtRealisticScale fail if
// `ORDER BY valid_at ASC` is removed from ForComponent's query — the index
// supplies the same ordering for free, every time, as long as the planner
// reaches for it, which it reliably will. Disabling enable_indexscan,
// enable_indexonlyscan and enable_bitmapscan for the role forces a sequential
// scan instead, whose output order is the chunk's physical (here: insertion)
// order — the one thing the reversed fixture actually controls. Only then
// does removing the ORDER BY produce observably wrong output. This was
// confirmed empirically, not assumed: see task-6-report.md for both runs.
func harnessWithoutCoveringIndex(t *testing.T) *riskfabriq.Store {
	t.Helper()

	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, riskfabriq.Group())

	appDSN := testsupport.AppDSN(t, superDSN)

	admin := pgdriver.New()
	if err := admin.Open(context.Background(), superDSN); err != nil {
		t.Fatalf("opening the admin connection to disable index scans: %v", err)
	}

	defer func() { _ = admin.Close() }()

	// ALTER ROLE ... SET is a login-time default: it only affects sessions
	// opened after this runs, which is why it happens before fab.Open below
	// rather than after. It is deliberately scoped to fabriq_app rather than
	// the whole database, so it cannot leak into any other role's plans.
	for _, stmt := range []string{
		`ALTER ROLE fabriq_app SET enable_indexscan = off`,
		`ALTER ROLE fabriq_app SET enable_indexonlyscan = off`,
		`ALTER ROLE fabriq_app SET enable_bitmapscan = off`,
	} {
		if _, err := admin.Exec(context.Background(), stmt); err != nil {
			t.Fatalf("disabling index scans for fabriq_app: %v", err)
		}
	}

	reg, err := entities.Build(riskfabriq.Contributor{})
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

	s, err := riskfabriq.New(fab, tenantID)
	if err != nil {
		t.Fatalf("building the store: %v", err)
	}

	return s
}

// aPass builds one component's 24-hour forecast, one row per hour, in
// ascending ValidAt order — the natural order a forecast pass computes in.
func aPass(component id.ID, quantity store.Quantity, at time.Time) []store.ComponentValue {
	out := make([]store.ComponentValue, 0, 24)

	for hour := range 24 {
		out = append(out, store.ComponentValue{
			ComponentID: component,
			Quantity:    quantity,
			IssuedAt:    at,
			ValidAt:     at.Add(time.Duration(hour) * time.Hour),
			Probability: 0.01 * float64(hour+1),
			Scalar:      1,
		})
	}

	return out
}

// reversed copies vs in reverse order.
//
// TestValuesRoundTripAtRealisticScale inserts every pass through this rather
// than aPass's natural ascending order. If the rows land in the table
// already sorted the way ForComponent's `ORDER BY valid_at ASC` is supposed
// to produce them, the composite index on (tenant_id, component_id,
// quantity, issued_at, valid_at) — an exact match for that query's WHERE
// clause — can return them correctly ordered by sheer coincidence of index
// or physical order, whether or not the ORDER BY clause is actually present
// in values.go. Reversing the insertion order means a missing or broken
// ORDER BY produces detectably wrong (descending) output instead. Do not
// "tidy" this back to ascending — that would silently turn the ordering
// assertion below back into one that cannot fail.
func reversed(vs []store.ComponentValue) []store.ComponentValue {
	out := make([]store.ComponentValue, len(vs))
	for i, v := range vs {
		out[len(vs)-1-i] = v
	}

	return out
}

// TestValuesRoundTripAtRealisticScale writes a batch shaped like an actual
// risk pass — one row per component per forecast hour — sized well past the
// 500-row PutBatch chunk boundary rather than the handful of rows that would
// prove nothing about the chunked path. 50 spans of 24 hours each is 1,200
// rows, three chunks (500, 500, 200), and the boundary lands inside a
// component's own series (component 20 occupies rows 480-503) so a chunk
// split at the wrong place would show up as a truncated read for exactly
// that component.
//
// Each component's pass is inserted through reversed (see its doc comment)
// rather than aPass's natural ascending order, and the harness disables the
// planner's access to the covering index (see harnessWithoutCoveringIndex):
// together, neither the fixture's physical order nor an incidental index
// scan can produce the expected ascending output on their own, so the
// assertion below only passes because ForComponent's own ORDER BY does.
func TestValuesRoundTripAtRealisticScale(t *testing.T) {
	s := harnessWithoutCoveringIndex(t)
	ctx := context.Background()

	const components = 50

	spans := make([]id.ID, components)
	for i := range spans {
		spans[i] = gid.NewSpan()
	}

	batch := make([]store.ComponentValue, 0, components*24)
	for _, span := range spans {
		batch = append(batch, reversed(aPass(span, store.QuantityConductorFailure, issuance))...)
	}

	if len(batch) <= 1000 {
		t.Fatalf("test batch has %d rows, want more than 1000 to exercise the chunked path", len(batch))
	}

	if err := s.Values().PutBatch(ctx, batch); err != nil {
		t.Fatalf("PutBatch: %v", err)
	}

	const boundaryComponent = 20

	got, err := s.Values().ForComponent(ctx, spans[boundaryComponent],
		[]store.Quantity{store.QuantityConductorFailure}, issuance, issuance, issuance.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("ForComponent: %v", err)
	}

	if len(got) != 24 {
		t.Fatalf("read %d hours for the component straddling the chunk boundary, want 24", len(got))
	}

	for hour, value := range got {
		want := issuance.Add(time.Duration(hour) * time.Hour)
		if !value.ValidAt.Equal(want) {
			t.Errorf("hour %d: ValidAt = %v, want %v (order or contents corrupted at the boundary)",
				hour, value.ValidAt, want)
		}
	}
}

// An issuance is its own series: two passes over the same component keep
// their rows apart by IssuedAt, matching
// extensions/risk/store/memory/memory.go's map key.
func TestAnIssuanceIsItsOwnSeries(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	span := gid.NewSpan()
	laterIssuance := issuance.Add(24 * time.Hour)

	first := aPass(span, store.QuantityConductorFailure, issuance)
	second := aPass(span, store.QuantityConductorFailure, laterIssuance)

	if err := s.Values().PutBatch(ctx, append(first, second...)); err != nil {
		t.Fatalf("PutBatch: %v", err)
	}

	got, err := s.Values().ForComponent(ctx, span, []store.Quantity{store.QuantityConductorFailure},
		issuance, issuance, issuance.Add(48*time.Hour))
	if err != nil {
		t.Fatalf("ForComponent: %v", err)
	}

	if len(got) != len(first) {
		t.Fatalf("ForComponent under the first issuance returned %d rows, want %d — "+
			"the second issuance's rows leaked in", len(got), len(first))
	}

	for _, value := range got {
		if !value.IssuedAt.Equal(issuance) {
			t.Errorf("row valid at %v carries issuance %v, want %v", value.ValidAt, value.IssuedAt, issuance)
		}
	}
}

// A degenerate window is legitimately empty, not an error — matching the
// memory store, the brief's named behavioral reference, whose inWindow
// predicate can never be true when to <= from.
func TestADegenerateWindowReturnsEmpty(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	span := gid.NewSpan()

	if err := s.Values().PutBatch(ctx, aPass(span, store.QuantityStructureFailure, issuance)); err != nil {
		t.Fatalf("PutBatch: %v", err)
	}

	got, err := s.Values().ForComponent(ctx, span, []store.Quantity{store.QuantityStructureFailure},
		issuance, issuance, issuance)
	if err != nil {
		t.Fatalf("ForComponent with an empty window returned an error: %v", err)
	}

	if len(got) != 0 {
		t.Errorf("ForComponent with from == to returned %d rows, want 0", len(got))
	}
}

// TestForComponentFiltersByQuantity proves the `AND quantity IN (...)`
// clause in ForComponent's WHERE clause (values.go:131) actually narrows the
// read. Every other fixture in this file asks ForComponent for exactly one
// quantity per component, so deleting that clause entirely would leave every
// one of them green — a component with two quantities at the same issuance
// and moment is the only shape that can catch it.
func TestForComponentFiltersByQuantity(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	span := gid.NewSpan()

	if err := s.Values().PutBatch(ctx, []store.ComponentValue{
		{
			ComponentID: span, Quantity: store.QuantityConductorFailure,
			IssuedAt: issuance, ValidAt: issuance, Probability: 0.11, Scalar: 1,
		},
		{
			ComponentID: span, Quantity: store.QuantityStructureFailure,
			IssuedAt: issuance, ValidAt: issuance, Probability: 0.99, Scalar: 1,
		},
	}); err != nil {
		t.Fatalf("PutBatch: %v", err)
	}

	got, err := s.Values().ForComponent(ctx, span,
		[]store.Quantity{store.QuantityConductorFailure},
		issuance, issuance, issuance.Add(time.Hour))
	if err != nil {
		t.Fatalf("ForComponent: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("asking for QuantityConductorFailure alone returned %d rows, "+
			"want 1 — the structure row on the same component leaked in", len(got))
	}

	if got[0].Quantity != store.QuantityConductorFailure || got[0].Probability != 0.11 {
		t.Fatalf("got %+v, want the conductor row alone", got[0])
	}
}

// TestValidAtUpperBoundIsExclusive proves the half-open `valid_at < $4`
// bound in ForComponent's WHERE clause (values.go:130): a row written
// exactly at the window's `to` must not come back. Every other fixture in
// this file writes rows strictly inside its query window, so flipping that
// operator to `<=` would leave them all green.
func TestValidAtUpperBoundIsExclusive(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	span := gid.NewSpan()
	inside := issuance.Add(time.Hour)
	to := issuance.Add(3 * time.Hour)

	if err := s.Values().PutBatch(ctx, []store.ComponentValue{
		{
			ComponentID: span, Quantity: store.QuantityConductorFailure,
			IssuedAt: issuance, ValidAt: inside, Probability: 0.2, Scalar: 1,
		},
		{
			// Exactly at the query's `to` — must be excluded.
			ComponentID: span, Quantity: store.QuantityConductorFailure,
			IssuedAt: issuance, ValidAt: to, Probability: 0.9, Scalar: 1,
		},
	}); err != nil {
		t.Fatalf("PutBatch: %v", err)
	}

	got, err := s.Values().ForComponent(ctx, span,
		[]store.Quantity{store.QuantityConductorFailure}, issuance, issuance, to)
	if err != nil {
		t.Fatalf("ForComponent: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("read %d rows for [issuance, to), want 1 — the row at exactly "+
			"`to` should fall outside a half-open window", len(got))
	}

	if !got[0].ValidAt.Equal(inside) {
		t.Errorf("got ValidAt %v, want %v", got[0].ValidAt, inside)
	}
}

// TestANonDefaultScalarIsReadBack pins the scalar column at the store it is
// actually kept in. scalar DOUBLE PRECISION NOT NULL DEFAULT 1
// (migrations.go:62) is the entire justification named in the package doc
// for bypassing Fabriq's Timeseries port, yet until now no fabriq test read
// back got.Scalar — every fixture in this file writes Scalar: 1, which is
// indistinguishable from the column default silently supplying a value
// valueToModel never set. A non-default scalar is the only fixture that can
// tell those two apart.
func TestANonDefaultScalarIsReadBack(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	span := gid.NewSpan()
	const scalar = 0.7

	if err := s.Values().PutBatch(ctx, []store.ComponentValue{
		{
			ComponentID: span, Quantity: store.QuantityConductorFailure,
			IssuedAt: issuance, ValidAt: issuance, Probability: 0.3, Scalar: scalar,
		},
	}); err != nil {
		t.Fatalf("PutBatch: %v", err)
	}

	got, err := s.Values().ForComponent(ctx, span,
		[]store.Quantity{store.QuantityConductorFailure},
		issuance, issuance, issuance.Add(time.Hour))
	if err != nil {
		t.Fatalf("ForComponent: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("read %d rows, want 1", len(got))
	}

	if got[0].Scalar != scalar {
		t.Errorf("read back Scalar %v, want %v — the column default (1) "+
			"masking a value that was never actually written would look "+
			"identical to every other fixture in this file", got[0].Scalar, scalar)
	}
}

// The value table carries row-level security (migrations.go: ENABLE and
// FORCE, not merely a policy left to the owner's discretion). This is the
// test that proves the policy actually filters rows rather than merely
// existing in the migration text — and it only can because the connection
// underneath both stores is fabriq_app, the non-superuser role
// testsupport.AppDSN provisions. A superuser connection bypasses row
// security entirely (BYPASSRLS is implicit for superusers), so the same test
// run against the migrating role would pass whether or not the policy did
// anything.
func TestValueRowsAreIsolatedByTenant(t *testing.T) {
	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, riskfabriq.Group())

	appDSN := testsupport.AppDSN(t, superDSN)

	reg, err := entities.Build(riskfabriq.Contributor{})
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

	owner, err := riskfabriq.New(fab, tenantID)
	if err != nil {
		t.Fatalf("building the owning tenant's store: %v", err)
	}

	other, err := riskfabriq.New(fab, "othertenant")
	if err != nil {
		t.Fatalf("building the other tenant's store: %v", err)
	}

	ctx := context.Background()
	span := gid.NewSpan()

	value := store.ComponentValue{
		ComponentID: span,
		Quantity:    store.QuantityConductorFailure,
		IssuedAt:    issuance,
		ValidAt:     issuance,
		Probability: 0.42,
		Scalar:      1,
	}

	if err := owner.Values().PutBatch(ctx, []store.ComponentValue{value}); err != nil {
		t.Fatalf("PutBatch under the owning tenant: %v", err)
	}

	keys := []store.Quantity{store.QuantityConductorFailure}
	window := issuance.Add(time.Hour)

	gotOwner, err := owner.Values().ForComponent(ctx, span, keys, issuance, issuance, window)
	if err != nil {
		t.Fatalf("ForComponent as the owning tenant: %v", err)
	}

	if len(gotOwner) != 1 {
		t.Fatalf("the owning tenant read %d rows for its own write, want 1", len(gotOwner))
	}

	gotOther, err := other.Values().ForComponent(ctx, span, keys, issuance, issuance, window)
	if err != nil {
		t.Fatalf("ForComponent as the other tenant: %v", err)
	}

	if len(gotOther) != 0 {
		t.Errorf("the other tenant read %d rows across the tenant boundary, want 0", len(gotOther))
	}
}
