package hazard_test

import (
	"testing"
	"time"

	"github.com/xraph/blackstart/extensions/hazard"
	"github.com/xraph/blackstart/extensions/hazard/store/memory"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

func computer(t *testing.T, w *world, config hazard.ComputeConfig) *hazard.Computer {
	t.Helper()

	got, err := hazard.NewComputer(w.engine(t), config, nil)
	if err != nil {
		t.Fatalf("NewComputer: %v", err)
	}

	got.SetClock(issuance)

	return got
}

func aComputeConfig() hazard.ComputeConfig {
	return hazard.ComputeConfig{
		Enabled:  true,
		Interval: time.Minute,
		Lookback: 12 * time.Hour,
	}
}

// A sampled cycle nobody has rated gets rated. This is the whole loop: without
// it a forecast lands, gets sampled, and produces no rating until somebody
// types a command.
func TestATickRatesASampledCycle(t *testing.T) {
	t.Parallel()

	w := newWorld(t).withWeather(icyWeather())
	w.weather.cycle.Status = forecast.StatusSampled

	if err := computer(t, w, aComputeConfig()).Tick(t.Context()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	got, err := w.store.Computations().List(t.Context(), pkgstore.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("recorded %d computations, want 1", len(got))
	}

	if got[0].CycleID != w.weather.cycle.ID {
		t.Errorf("computation names cycle %s, want %s",
			got[0].CycleID, w.weather.cycle.ID)
	}

	if got[0].Values == 0 {
		t.Error("the tick rated the cycle and wrote no values")
	}
}

// A cycle that has been rated is not rated again. Otherwise a two-minute loop
// would recompute the whole network forever and leave a provenance run per
// tick.
func TestASecondTickRatesNothingNew(t *testing.T) {
	t.Parallel()

	w := newWorld(t).withWeather(icyWeather())
	w.weather.cycle.Status = forecast.StatusSampled

	c := computer(t, w, aComputeConfig())

	if err := c.Tick(t.Context()); err != nil {
		t.Fatalf("first Tick: %v", err)
	}

	if err := c.Tick(t.Context()); err != nil {
		t.Fatalf("second Tick: %v", err)
	}

	got, err := w.store.Computations().List(t.Context(), pkgstore.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(got) != 1 {
		t.Errorf("two ticks recorded %d computations, want 1", len(got))
	}
}

// Only sampled cycles are candidates. A fetched one has no per-span samples
// yet, so rating it would produce a computation covering nothing and mark the
// cycle as done.
func TestAFetchedCycleIsNotRated(t *testing.T) {
	t.Parallel()

	w := newWorld(t).withWeather(icyWeather())
	w.weather.cycle.Status = forecast.StatusFetched

	if err := computer(t, w, aComputeConfig()).Tick(t.Context()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	got, err := w.store.Computations().List(t.Context(), pkgstore.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(got) != 0 {
		t.Errorf("rated %d cycles that were never sampled", len(got))
	}
}

// A cycle older than the lookback is left alone. An unrated forecast from last
// week is not an operational problem, and sweeping all history every tick would
// grow without bound.
func TestACycleOutsideTheLookbackIsLeftAlone(t *testing.T) {
	t.Parallel()

	w := newWorld(t).withWeather(icyWeather())
	w.weather.cycle.Status = forecast.StatusSampled
	w.weather.cycle.IssuedAt = issuance().Add(-48 * time.Hour)

	config := aComputeConfig()
	config.Lookback = time.Hour

	if err := computer(t, w, config).Tick(t.Context()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	got, err := w.store.Computations().List(t.Context(), pkgstore.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(got) != 0 {
		t.Errorf("rated %d cycles from outside the lookback", len(got))
	}
}

// An engine built for reads has nothing to sweep and says so by doing nothing,
// rather than erroring every two minutes for the life of the process.
func TestAReadOnlyEngineTicksQuietly(t *testing.T) {
	t.Parallel()

	engine, err := hazard.NewEngine(memory.New(), nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	c, err := hazard.NewComputer(engine, aComputeConfig(), nil)
	if err != nil {
		t.Fatalf("NewComputer: %v", err)
	}

	if err := c.Tick(t.Context()); err != nil {
		t.Errorf("Tick on a read-only engine: %v", err)
	}
}

func TestAComputerNeedsAnEngine(t *testing.T) {
	t.Parallel()

	if _, err := hazard.NewComputer(nil, aComputeConfig(), nil); err == nil {
		t.Fatal("a computer was built with no engine")
	}
}

func TestDefaultComputeConfigIsUsable(t *testing.T) {
	t.Parallel()

	got := hazard.DefaultComputeConfig()

	if got.Enabled {
		t.Error("the default enables the loop; the binary should decide that")
	}

	if got.Interval <= 0 || got.Lookback <= 0 {
		t.Errorf("the default is not usable: %+v", got)
	}

	// Shorter than the weather poller's, because this loop makes no network
	// requests — the cost of asking often is one query.
	if got.Interval >= 10*time.Minute {
		t.Errorf("the recompute interval is %v, no shorter than the weather "+
			"poll it follows", got.Interval)
	}
}

// Zero values are replaced rather than taken literally, so a partially filled
// config does not produce a loop that ticks in a tight spin or sweeps nothing.
func TestAZeroScheduleFallsBackToTheDefault(t *testing.T) {
	t.Parallel()

	w := newWorld(t).withWeather(icyWeather())
	w.weather.cycle.Status = forecast.StatusSampled

	c, err := hazard.NewComputer(w.engine(t), hazard.ComputeConfig{Enabled: true}, nil)
	if err != nil {
		t.Fatalf("NewComputer: %v", err)
	}

	c.SetClock(issuance)

	if err := c.Tick(t.Context()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	got, err := w.store.Computations().List(t.Context(), pkgstore.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(got) != 1 {
		t.Errorf("a zero-valued schedule rated %d cycles, want 1", len(got))
	}
}
