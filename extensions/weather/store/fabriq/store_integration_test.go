//go:build integration

package fabriq_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xraph/fabriq"

	dsid "github.com/xraph/blackstart/extensions/datasource/id"
	dsfabriq "github.com/xraph/blackstart/extensions/datasource/store/fabriq"
	"github.com/xraph/blackstart/extensions/geo/shape"
	pvid "github.com/xraph/blackstart/extensions/provenance/id"
	"github.com/xraph/blackstart/extensions/weather/field"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	wid "github.com/xraph/blackstart/extensions/weather/id"
	"github.com/xraph/blackstart/extensions/weather/observation"
	wxfabriq "github.com/xraph/blackstart/extensions/weather/store/fabriq"
	"github.com/xraph/blackstart/internal/entities"
	"github.com/xraph/blackstart/internal/testsupport"
	"github.com/xraph/blackstart/pkg/bserr"
)

const tenantID = "blackstart"

var issuance = time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

// harness brings up Postgres, runs both migration groups through one
// orchestrator, provisions the restricted application role, and opens Fabriq.
//
// The datasource group comes along because a cycle's feed_id edge targets
// datasource_feed, and the registry refuses an edge whose target it has never
// heard of.
func harness(t *testing.T) *wxfabriq.Store {
	t.Helper()

	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, dsfabriq.Group(), wxfabriq.Group())

	// The application connects as a non-superuser, because PostgreSQL row
	// security never constrains a superuser — a test that connected as one
	// would pass every isolation check while proving nothing.
	appDSN := testsupport.AppDSN(t, superDSN)

	reg, err := entities.Build(dsfabriq.Contributor{}, wxfabriq.Contributor{})
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

	s, err := wxfabriq.New(fab, tenantID)
	if err != nil {
		t.Fatalf("building the store: %v", err)
	}

	return s
}

func aCycle() forecast.Cycle {
	return forecast.Cycle{
		ID:       wid.NewCycle(),
		FeedID:   dsid.NewFeed(),
		Model:    forecast.ModelHRRR,
		IssuedAt: issuance,
		Status:   forecast.StatusPending,
	}
}

func TestCycleRoundTrip(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	want := aCycle()

	if err := s.Cycles().Create(ctx, want); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Cycles().Get(ctx, want.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.ID != want.ID || got.Model != want.Model || got.Status != want.Status {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}

	// The instant must survive a timestamptz round trip, because the unique
	// index that makes a cycle idempotent compares it.
	if !got.IssuedAt.Equal(want.IssuedAt) {
		t.Errorf("issuance = %s, want %s", got.IssuedAt, want.IssuedAt)
	}
}

// The unique index is the authority on idempotency, not a read-then-write
// check — which a concurrent backfill and poll would both pass. This is the
// assertion that only a real database can make.
func TestDuplicateIssuanceConflicts(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	if err := s.Cycles().Create(ctx, aCycle()); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	err := s.Cycles().Create(ctx, aCycle())
	if !errors.Is(err, bserr.ErrConflict) {
		t.Fatalf("second Create error = %v, want ErrConflict", err)
	}
}

func TestForIssuanceFindsByNaturalKey(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	want := aCycle()

	if err := s.Cycles().Create(ctx, want); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Cycles().ForIssuance(ctx, forecast.ModelHRRR, issuance)
	if err != nil {
		t.Fatalf("ForIssuance: %v", err)
	}

	if got.ID != want.ID {
		t.Errorf("ForIssuance returned %s, want %s", got.ID, want.ID)
	}

	// An issuance expressed in another zone is the same instant and must find
	// the same row, or a worker in a non-UTC deployment would re-fetch every
	// cycle it already had.
	elsewhere := issuance.In(time.FixedZone("test", 5*60*60))

	got, err = s.Cycles().ForIssuance(ctx, forecast.ModelHRRR, elsewhere)
	if err != nil {
		t.Fatalf("ForIssuance with a non-UTC instant: %v", err)
	}

	if got.ID != want.ID {
		t.Errorf("the same instant in another zone found %s, want %s",
			got.ID, want.ID)
	}
}

func TestSetStatusPreservesTheRest(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	cycle := aCycle()

	if err := s.Cycles().Create(ctx, cycle); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := s.Cycles().SetStatus(ctx, cycle.ID, forecast.StatusSampled); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	got, err := s.Cycles().Get(ctx, cycle.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Status != forecast.StatusSampled {
		t.Errorf("status = %q, want sampled", got.Status)
	}

	// The command path replaces the aggregate, so a partial payload would have
	// blanked these. This is what the read-modify-write in SetStatus is for.
	if got.FeedID != cycle.FeedID || got.Model != cycle.Model {
		t.Errorf("SetStatus blanked other columns: %+v", got)
	}
}

func TestGridRoundTripAndSampledNull(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	cycle := aCycle()

	if err := s.Cycles().Create(ctx, cycle); err != nil {
		t.Fatalf("Create cycle: %v", err)
	}

	grid := forecast.Grid{
		ID:               wid.NewGrid(),
		CycleID:          cycle.ID,
		ForecastHour:     6,
		Field:            field.Gust10m,
		BlobRef:          "sha256:abc",
		DatasetVersionID: pvid.NewDataset(),
		NX:               1799,
		NY:               1059,
		Projection:       "lambert",
	}

	if err := s.Grids().Create(ctx, grid); err != nil {
		t.Fatalf("Create grid: %v", err)
	}

	got, err := s.Grids().Get(ctx, grid.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// A grid that has never been sampled stores NULL, which must read back as
	// the zero time rather than as the year 1.
	if !got.SampledAt.IsZero() {
		t.Errorf("an unsampled grid read back SampledAt = %s", got.SampledAt)
	}

	at := issuance.Add(time.Minute)

	if err := s.Grids().MarkSampled(ctx, grid.ID, at, 480, 512); err != nil {
		t.Fatalf("MarkSampled: %v", err)
	}

	got, err = s.Grids().Get(ctx, grid.ID)
	if err != nil {
		t.Fatalf("Get after MarkSampled: %v", err)
	}

	if !got.SampledAt.Equal(at) || got.Covered != 480 || got.Total != 512 {
		t.Errorf("after MarkSampled: %+v", got)
	}

	// The two fields that make a sample traceable must survive the update.
	if got.BlobRef != grid.BlobRef || got.DatasetVersionID != grid.DatasetVersionID {
		t.Errorf("MarkSampled blanked the provenance columns: %+v", got)
	}
}

func TestDuplicateGridFieldConflicts(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	cycle := aCycle()

	if err := s.Cycles().Create(ctx, cycle); err != nil {
		t.Fatalf("Create cycle: %v", err)
	}

	grid := func() forecast.Grid {
		return forecast.Grid{
			ID:               wid.NewGrid(),
			CycleID:          cycle.ID,
			ForecastHour:     6,
			Field:            field.Gust10m,
			BlobRef:          "sha256:abc",
			DatasetVersionID: pvid.NewDataset(),
		}
	}

	if err := s.Grids().Create(ctx, grid()); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	err := s.Grids().Create(ctx, grid())
	if !errors.Is(err, bserr.ErrConflict) {
		t.Fatalf("second Create error = %v, want ErrConflict; two dataset "+
			"versions for one field would leave nothing to say which the "+
			"samples came from", err)
	}
}

func TestStationRoundTripAndReseed(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	station := observation.Station{
		ID:       wid.NewStation(),
		Code:     "KAUS",
		Name:     "Austin",
		Location: shape.Point{Lon: -97.67, Lat: 30.19, Elevation: 165},
	}

	if err := s.Stations().Put(ctx, station); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// A reseed writes the same identifier again. It must not be an error, or
	// every refresh of the station list would look like a failure.
	station.Name = "Austin-Bergstrom International"

	if err := s.Stations().Put(ctx, station); err != nil {
		t.Fatalf("reseed Put: %v", err)
	}

	got, err := s.Stations().ByCode(ctx, "KAUS")
	if err != nil {
		t.Fatalf("ByCode: %v", err)
	}

	if got.Name != "Austin-Bergstrom International" {
		t.Errorf("name = %q; the reseed did not replace", got.Name)
	}

	if got.Location != station.Location {
		t.Errorf("location = %+v, want %+v", got.Location, station.Location)
	}
}
