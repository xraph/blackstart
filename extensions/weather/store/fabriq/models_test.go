package fabriq

import (
	"testing"
	"time"

	dsid "github.com/xraph/blackstart/extensions/datasource/id"
	"github.com/xraph/blackstart/extensions/geo/shape"
	pvid "github.com/xraph/blackstart/extensions/provenance/id"
	"github.com/xraph/blackstart/extensions/weather/field"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	wid "github.com/xraph/blackstart/extensions/weather/id"
	"github.com/xraph/blackstart/extensions/weather/observation"
	"github.com/xraph/blackstart/extensions/weather/store"
)

// The Fabriq store must satisfy the same interface the memory one does, or the
// engine tested against memory proves nothing about production.
var _ store.Store = (*Store)(nil)

var issuance = time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

func TestCycleModelRoundTrips(t *testing.T) {
	t.Parallel()

	want := forecast.Cycle{
		ID:       wid.NewCycle(),
		FeedID:   dsid.NewFeed(),
		Model:    forecast.ModelHRRR,
		IssuedAt: issuance,
		Status:   forecast.StatusFetched,
	}

	got, err := cycleFromModel(*cycleToModel(want))
	if err != nil {
		t.Fatalf("cycleFromModel: %v", err)
	}

	if got.ID != want.ID || got.FeedID != want.FeedID ||
		got.Model != want.Model || got.Status != want.Status {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}

	if !got.IssuedAt.Equal(want.IssuedAt) {
		t.Errorf("issuance = %s, want %s", got.IssuedAt, want.IssuedAt)
	}
}

// Issuance is normalised to UTC on the way in, because the unique index that
// makes a cycle idempotent compares the stored value — and a comparison
// against the same instant in another zone is one nobody can reason about.
func TestCycleModelNormalizesIssuanceToUTC(t *testing.T) {
	t.Parallel()

	elsewhere := issuance.In(time.FixedZone("test", 5*60*60))

	model := cycleToModel(forecast.Cycle{
		ID:       wid.NewCycle(),
		FeedID:   dsid.NewFeed(),
		Model:    forecast.ModelHRRR,
		IssuedAt: elsewhere,
		Status:   forecast.StatusPending,
	})

	if model.IssuedAt.Location() != time.UTC {
		t.Errorf("stored issuance is in %s, want UTC", model.IssuedAt.Location())
	}

	if !model.IssuedAt.Equal(issuance) {
		t.Errorf("normalising changed the instant: %s, want %s",
			model.IssuedAt, issuance)
	}
}

// A grid that has never been sampled must store NULL, not a zero timestamp.
// The year 1 is a value, and a wrong one — it would make an unsampled grid
// look sampled a very long time ago.
func TestUnsampledGridStoresNull(t *testing.T) {
	t.Parallel()

	model := gridToModel(forecast.Grid{
		ID:               wid.NewGrid(),
		CycleID:          wid.NewCycle(),
		Field:            field.Gust10m,
		BlobRef:          "sha256:abc",
		DatasetVersionID: pvid.NewDataset(),
	})

	if model.SampledAt != nil {
		t.Fatalf("an unsampled grid stored %v, want NULL", *model.SampledAt)
	}

	got, err := gridFromModel(*model)
	if err != nil {
		t.Fatalf("gridFromModel: %v", err)
	}

	if !got.SampledAt.IsZero() {
		t.Errorf("NULL read back as %s, want the zero time", got.SampledAt)
	}
}

func TestSampledGridRoundTrips(t *testing.T) {
	t.Parallel()

	at := issuance.Add(time.Minute)

	want := forecast.Grid{
		ID:               wid.NewGrid(),
		CycleID:          wid.NewCycle(),
		ForecastHour:     6,
		Field:            field.FreezingRain,
		BlobRef:          "sha256:abc",
		DatasetVersionID: pvid.NewDataset(),
		NX:               1799,
		NY:               1059,
		Projection:       "lambert",
		SampledAt:        at,
		Covered:          480,
		Total:            512,
	}

	got, err := gridFromModel(*gridToModel(want))
	if err != nil {
		t.Fatalf("gridFromModel: %v", err)
	}

	if got.Field != want.Field || got.ForecastHour != want.ForecastHour ||
		got.BlobRef != want.BlobRef || got.DatasetVersionID != want.DatasetVersionID {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}

	if got.Covered != want.Covered || got.Total != want.Total {
		t.Errorf("covered/total = %d/%d, want %d/%d",
			got.Covered, got.Total, want.Covered, want.Total)
	}

	if !got.SampledAt.Equal(at) {
		t.Errorf("SampledAt = %s, want %s", got.SampledAt, at)
	}
}

func TestStationModelRoundTrips(t *testing.T) {
	t.Parallel()

	want := observation.Station{
		ID:       wid.NewStation(),
		Code:     "KAUS",
		Name:     "Austin-Bergstrom",
		Location: shape.Point{Lon: -97.67, Lat: 30.19, Elevation: 165},
	}

	got, err := stationFromModel(*stationToModel(want))
	if err != nil {
		t.Fatalf("stationFromModel: %v", err)
	}

	if got != want {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

// The ordinates are columns rather than well-known text, so a station's
// location survives without a parse step. This pins that: a WKT round trip
// would be a different failure mode entirely.
func TestStationOrdinatesAreStoredAsNumbers(t *testing.T) {
	t.Parallel()

	model := stationToModel(observation.Station{
		ID:       wid.NewStation(),
		Code:     "KAUS",
		Location: shape.Point{Lon: -97.67, Lat: 30.19},
	})

	if model.Lon != -97.67 || model.Lat != 30.19 {
		t.Errorf("stored lon/lat = %v/%v, want -97.67/30.19", model.Lon, model.Lat)
	}
}
