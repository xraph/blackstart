//go:build integration

package fabriq_test

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/fabriq"

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

// aPass builds one component's 24-hour forecast, one row per hour.
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

// TestValuesRoundTripAtRealisticScale writes a batch shaped like an actual
// risk pass — one row per component per forecast hour — sized well past the
// 500-row PutBatch chunk boundary rather than the handful of rows that would
// prove nothing about the chunked path. 50 spans of 24 hours each is 1,200
// rows, three chunks (500, 500, 200), and the boundary lands inside a
// component's own series (component 20 occupies rows 480-503) so a chunk
// split at the wrong place would show up as a truncated read for exactly
// that component.
func TestValuesRoundTripAtRealisticScale(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	const components = 50

	spans := make([]id.ID, components)
	for i := range spans {
		spans[i] = gid.NewSpan()
	}

	batch := make([]store.ComponentValue, 0, components*24)
	for _, span := range spans {
		batch = append(batch, aPass(span, store.QuantityConductorFailure, issuance)...)
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
