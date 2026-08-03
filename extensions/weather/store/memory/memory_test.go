package memory_test

import (
	"testing"
	"time"

	dsid "github.com/xraph/blackstart/extensions/datasource/id"
	pvid "github.com/xraph/blackstart/extensions/provenance/id"
	"github.com/xraph/blackstart/extensions/weather/field"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	wid "github.com/xraph/blackstart/extensions/weather/id"
	"github.com/xraph/blackstart/extensions/weather/store/memory"
)

var issuance = time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

func aCycle() forecast.Cycle {
	return forecast.Cycle{
		ID:       wid.NewCycle(),
		FeedID:   dsid.NewFeed(),
		Model:    forecast.ModelHRRR,
		IssuedAt: issuance,
		Status:   forecast.StatusPending,
	}
}

func aGrid(cycle forecast.Cycle, hour int, f field.Field) forecast.Grid {
	return forecast.Grid{
		ID:               wid.NewGrid(),
		CycleID:          cycle.ID,
		ForecastHour:     hour,
		Field:            f,
		BlobRef:          "sha256:abc",
		DatasetVersionID: pvid.NewDataset(),
		NX:               1799,
		NY:               1059,
		Projection:       "lambert",
	}
}

func TestLifecycle(t *testing.T) {
	t.Parallel()

	s := memory.New()

	if err := s.Migrate(t.Context()); err != nil {
		t.Errorf("Migrate: %v", err)
	}

	if err := s.Ping(t.Context()); err != nil {
		t.Errorf("Ping: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}
