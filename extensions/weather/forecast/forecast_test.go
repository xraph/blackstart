package forecast_test

import (
	"errors"
	"testing"
	"time"

	dsid "github.com/xraph/blackstart/extensions/datasource/id"
	pvid "github.com/xraph/blackstart/extensions/provenance/id"
	"github.com/xraph/blackstart/extensions/weather/field"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	wid "github.com/xraph/blackstart/extensions/weather/id"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

func TestCycleValidation(t *testing.T) {
	t.Parallel()

	valid := forecast.Cycle{
		ID:       wid.NewCycle(),
		FeedID:   dsid.NewFeed(),
		Model:    forecast.ModelHRRR,
		IssuedAt: time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC),
		Status:   forecast.StatusPending,
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("a well-formed cycle was rejected: %v", err)
	}

	for name, mutate := range map[string]func(*forecast.Cycle){
		"no feed":     func(c *forecast.Cycle) { c.FeedID = id.Nil },
		"no model":    func(c *forecast.Cycle) { c.Model = "" },
		"bad model":   func(c *forecast.Cycle) { c.Model = "ecmwf" },
		"no issuance": func(c *forecast.Cycle) { c.IssuedAt = time.Time{} },
		"bad status":  func(c *forecast.Cycle) { c.Status = "halfway" },
	} {
		broken := valid
		mutate(&broken)

		err := broken.Validate()
		if err == nil {
			t.Errorf("%s: validated", name)

			continue
		}

		if !errors.Is(err, bserr.ErrInvalid) {
			t.Errorf("%s: error = %v, want bserr.ErrInvalid", name, err)
		}
	}
}

func TestGridValidation(t *testing.T) {
	t.Parallel()

	valid := forecast.Grid{
		ID:               wid.NewGrid(),
		CycleID:          wid.NewCycle(),
		ForecastHour:     6,
		Field:            field.Gust10m,
		BlobRef:          "blob_01",
		DatasetVersionID: pvid.NewDataset(),
		NX:               1799,
		NY:               1059,
		Projection:       "lambert",
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("a well-formed grid was rejected: %v", err)
	}

	for name, mutate := range map[string]func(*forecast.Grid){
		"no cycle":      func(g *forecast.Grid) { g.CycleID = id.Nil },
		"unknown field": func(g *forecast.Grid) { g.Field = "gust_10m" },
		"negative hour": func(g *forecast.Grid) { g.ForecastHour = -1 },
		"no blob":       func(g *forecast.Grid) { g.BlobRef = "" },
		"no dataset":    func(g *forecast.Grid) { g.DatasetVersionID = id.Nil },
	} {
		broken := valid
		mutate(&broken)

		if err := broken.Validate(); err == nil {
			t.Errorf("%s: validated", name)
		}
	}
}

// A forecast hour is hours after issuance, so the valid time it describes is
// derived rather than stored twice — two columns that must agree are two
// columns that eventually will not.
func TestGridValidAt(t *testing.T) {
	t.Parallel()

	issued := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	grid := forecast.Grid{ForecastHour: 18}

	want := time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC)

	if got := grid.ValidAt(issued); !got.Equal(want) {
		t.Errorf("ValidAt = %s, want %s", got, want)
	}
}

func TestModelSetIsClosed(t *testing.T) {
	t.Parallel()

	for _, m := range forecast.Models() {
		if !m.Valid() {
			t.Errorf("%q is in Models() but reports invalid", m)
		}
	}

	if forecast.Model("ecmwf").Valid() {
		t.Error("a model outside the registry reports valid")
	}
}
