package memory_test

import (
	"testing"
	"time"

	"github.com/xraph/blackstart/extensions/weather/field"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	"github.com/xraph/blackstart/extensions/weather/store/memory"
)

// Grids are read as a whole cycle, in order, so the order has to be the one
// the caller can rely on: forecast hour, then field.
func TestGridsForCycleAreOrdered(t *testing.T) {
	t.Parallel()

	s := memory.New()
	cycle := aCycle()

	if err := s.Cycles().Create(t.Context(), cycle); err != nil {
		t.Fatalf("Create: %v", err)
	}

	for _, g := range []forecast.Grid{
		aGrid(cycle, 3, field.Temp2m),
		aGrid(cycle, 1, field.Gust10m),
		aGrid(cycle, 3, field.Gust10m),
	} {
		if err := s.Grids().Create(t.Context(), g); err != nil {
			t.Fatalf("Create grid: %v", err)
		}
	}

	got, err := s.Grids().ForCycle(t.Context(), cycle.ID)
	if err != nil {
		t.Fatalf("ForCycle: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("ForCycle returned %d grids, want 3", len(got))
	}

	if got[0].ForecastHour != 1 {
		t.Errorf("first grid is hour %d, want 1", got[0].ForecastHour)
	}

	if got[1].ForecastHour != 3 || got[1].Field != field.Gust10m {
		t.Errorf("second grid is hour %d field %q, want 3 / gust10m",
			got[1].ForecastHour, got[1].Field)
	}
}

func TestGridMarkSampled(t *testing.T) {
	t.Parallel()

	s := memory.New()
	cycle := aCycle()
	grid := aGrid(cycle, 1, field.Gust10m)

	if err := s.Cycles().Create(t.Context(), cycle); err != nil {
		t.Fatalf("Create cycle: %v", err)
	}

	if err := s.Grids().Create(t.Context(), grid); err != nil {
		t.Fatalf("Create grid: %v", err)
	}

	at := issuance.Add(time.Minute)

	if err := s.Grids().MarkSampled(t.Context(), grid.ID, at, 480, 512); err != nil {
		t.Fatalf("MarkSampled: %v", err)
	}

	got, err := s.Grids().Get(t.Context(), grid.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if !got.SampledAt.Equal(at) {
		t.Errorf("SampledAt = %s, want %s", got.SampledAt, at)
	}

	// Both counts, so that 32 spans outside CONUS read as absence rather than
	// as a suspiciously round number.
	if got.Covered != 480 || got.Total != 512 {
		t.Errorf("covered/total = %d/%d, want 480/512", got.Covered, got.Total)
	}
}
