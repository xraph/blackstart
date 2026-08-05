//go:build integration

// This file exists because a fake cannot catch the bug it was written for.
//
// SpanAmpacities' whole job is to select one instant out of a series, and
// whether it does depends on a SQL predicate against a TIMESTAMPTZ column —
// a fake SeriesReader answers whatever it was given, whatever window it was
// asked for, and reports success on a window that matches nothing in
// Postgres. The first version of this adapter used a one-nanosecond window,
// which puts both bounds on the same microsecond on the wire and selects zero
// rows; every unit test passed and every real run would have failed at the
// rating step, blaming the forecast.
package scenario_test

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/fabriq"

	dsfabriq "github.com/xraph/blackstart/extensions/datasource/store/fabriq"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	hzstore "github.com/xraph/blackstart/extensions/hazard/store"
	hzfabriq "github.com/xraph/blackstart/extensions/hazard/store/fabriq"
	"github.com/xraph/blackstart/extensions/scenario"
	wxfabriq "github.com/xraph/blackstart/extensions/weather/store/fabriq"
	"github.com/xraph/blackstart/internal/entities"
	"github.com/xraph/blackstart/internal/testsupport"
	"github.com/xraph/blackstart/pkg/id"
)

const tenantID = "blackstart"

var (
	issuance = time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	validAt  = issuance.Add(6 * time.Hour)
)

// hazardHarness brings up Postgres with hazard's own schema and returns its
// value store — the real one this adapter reads through in production.
//
// The weather and datasource groups come along for the same reason hazard's
// own integration tests take them: a computation's cycle_id edge targets
// weather_cycle, whose feed_id edge targets datasource_feed, and the registry
// refuses an edge whose target it has never heard of.
func hazardHarness(t *testing.T) hzstore.ValueStore {
	t.Helper()

	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, dsfabriq.Group(), wxfabriq.Group(), hzfabriq.Group())

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

	store, err := hzfabriq.New(fab, tenantID)
	if err != nil {
		t.Fatalf("building the hazard store: %v", err)
	}

	return store.Values()
}

func ampacity(span id.ID, at time.Time, amperes float64) hzstore.SpanValue {
	return hzstore.SpanValue{
		SpanID:   span,
		Quantity: hzstore.QuantityAmpacity,
		IssuedAt: issuance,
		ValidAt:  at,
		Value:    amperes,
	}
}

// An ampacity written through hazard's own store reads back through this
// adapter at the exact instant it was written for.
//
// The test the nanosecond window fails. It is deliberately end-to-end through
// Postgres rather than through the ValueStore interface, because the bug was
// in the SQL the window compiles to and nothing above that layer can see it.
func TestAnAmpacityWrittenByHazardReadsBackAtItsInstant(t *testing.T) {
	values := hazardHarness(t)
	ctx := context.Background()

	first, second := id.MustNew(gid.Span), id.MustNew(gid.Span)

	if err := values.Write(ctx, []hzstore.SpanValue{
		ampacity(first, validAt, 912.5),
		ampacity(second, validAt, 1103.25),
	}); err != nil {
		t.Fatalf("writing the values: %v", err)
	}

	reader, err := scenario.NewSpanAmpacities(values)
	if err != nil {
		t.Fatalf("NewSpanAmpacities: %v", err)
	}

	got, err := reader.Ampacities(ctx, []id.ID{first, second}, issuance, validAt)
	if err != nil {
		t.Fatalf("Ampacities: %v", err)
	}

	if got[first] != 912.5 || got[second] != 1103.25 {
		t.Fatalf("read back %v, want both spans at the values hazard stored; a "+
			"window narrower than the column's resolution selects nothing and "+
			"every branch comes back unrated", got)
	}
}

// The neighboring forecast hours of the same series are not returned. One
// cycle holds a whole horizon under one key, and a run pinned to 18:00 that
// silently rated on 17:00 would be a real number for the wrong moment.
func TestTheNeighbouringForecastHoursAreNotRead(t *testing.T) {
	values := hazardHarness(t)
	ctx := context.Background()

	span := id.MustNew(gid.Span)

	if err := values.Write(ctx, []hzstore.SpanValue{
		ampacity(span, validAt.Add(-time.Hour), 400),
		ampacity(span, validAt, 900),
		ampacity(span, validAt.Add(time.Hour), 1500),
	}); err != nil {
		t.Fatalf("writing the series: %v", err)
	}

	reader, err := scenario.NewSpanAmpacities(values)
	if err != nil {
		t.Fatalf("NewSpanAmpacities: %v", err)
	}

	got, err := reader.Ampacities(ctx, []id.ID{span}, issuance, validAt)
	if err != nil {
		t.Fatalf("Ampacities: %v", err)
	}

	if got[span] != 900 {
		t.Errorf("got %v A, want 900 — the other two are the hours either side", got[span])
	}
}

// A span the pass never rated is absent rather than zero, through the real
// store as much as through a fake: a zero would become the branch's rating,
// and every consumer reads a non-positive rating as unrated and skips the
// branch rather than failing it.
func TestASpanHazardNeverRatedIsAbsentFromPostgresToo(t *testing.T) {
	values := hazardHarness(t)
	ctx := context.Background()

	rated, unrated := id.MustNew(gid.Span), id.MustNew(gid.Span)

	if err := values.Write(ctx, []hzstore.SpanValue{ampacity(rated, validAt, 900)}); err != nil {
		t.Fatalf("writing the value: %v", err)
	}

	reader, err := scenario.NewSpanAmpacities(values)
	if err != nil {
		t.Fatalf("NewSpanAmpacities: %v", err)
	}

	got, err := reader.Ampacities(ctx, []id.ID{rated, unrated}, issuance, validAt)
	if err != nil {
		t.Fatalf("Ampacities: %v", err)
	}

	if _, present := got[unrated]; present {
		t.Errorf("a span nothing rated came back as %v", got[unrated])
	}

	if len(got) != 1 {
		t.Errorf("got %d values for one rated span", len(got))
	}
}
