package weather_test

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/blackstart/extensions/weather"
	"github.com/xraph/blackstart/extensions/weather/field"
	"github.com/xraph/blackstart/extensions/weather/model"
	"github.com/xraph/blackstart/extensions/weather/store"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

func pollHarness(t *testing.T, config weather.PollConfig) (*weather.Poller, store.Store, *fakeFetcher) {
	t.Helper()

	fetcher := &fakeFetcher{}
	engine, s, _ := ingestHarness(t, fetcher)

	poller, err := weather.NewPoller(engine, config, nil)
	if err != nil {
		t.Fatalf("NewPoller: %v", err)
	}

	poller.SetClock(func() time.Time { return issuance })

	return poller, s, fetcher
}

func aPollConfig(backfill time.Duration) weather.PollConfig {
	return weather.PollConfig{
		Enabled:    true,
		Interval:   time.Minute,
		Backfill:   backfill,
		SourceSlug: "noaa",
		Feeds:      map[model.Model]string{model.HRRR: "hrrr"},
		Hours:      []int{1},
	}
}

// One tick over a six-hour horizon covers the newest cycle and the five below
// it — the bounded backfill the design settles on.
func TestATickWalksBackToTheHorizon(t *testing.T) {
	t.Parallel()

	poller, s, _ := pollHarness(t, aPollConfig(6*time.Hour))

	if err := poller.Tick(t.Context()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	cycles, err := s.Cycles().List(t.Context(), store.CycleFilter{}, pkgstore.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	// 12Z back through 06Z inclusive: seven hourly HRRR cycles.
	if len(cycles) != 7 {
		t.Fatalf("recorded %d cycles over six hours of backfill, want 7", len(cycles))
	}

	// Newest first, because operational correctness comes before filling the
	// record.
	if !cycles[0].IssuedAt.Equal(issuance) {
		t.Errorf("first cycle issued %s, want the newest at %s",
			cycles[0].IssuedAt, issuance)
	}
}

// A zero horizon is the demo setting: the newest cycle and nothing else.
func TestAZeroHorizonTakesOnlyTheNewest(t *testing.T) {
	t.Parallel()

	poller, s, _ := pollHarness(t, aPollConfig(0))

	if err := poller.Tick(t.Context()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	cycles, err := s.Cycles().List(t.Context(), store.CycleFilter{}, pkgstore.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(cycles) != 1 {
		t.Fatalf("recorded %d cycles with no backfill, want 1", len(cycles))
	}
}

// The second tick is what every restart does. It must record nothing new and
// re-fetch nothing, which is what makes running backfill unconditionally safe.
func TestASecondTickRecordsNothingNew(t *testing.T) {
	t.Parallel()

	poller, s, fetcher := pollHarness(t, aPollConfig(2*time.Hour))

	if err := poller.Tick(t.Context()); err != nil {
		t.Fatalf("first Tick: %v", err)
	}

	afterFirst, err := s.Cycles().List(t.Context(), store.CycleFilter{}, pkgstore.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	rangeCallsAfterFirst := fetcher.RangeCalls()

	if err := poller.Tick(t.Context()); err != nil {
		t.Fatalf("second Tick: %v", err)
	}

	afterSecond, err := s.Cycles().List(t.Context(), store.CycleFilter{}, pkgstore.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(afterSecond) != len(afterFirst) {
		t.Errorf("the second tick added %d cycles",
			len(afterSecond)-len(afterFirst))
	}

	// The bytes are already known by content hash, so nothing is stored again
	// — but they are still fetched, because knowing that requires hashing what
	// arrived. Cheapness comes from the skip, not from not asking.
	if fetcher.RangeCalls() <= rangeCallsAfterFirst {
		t.Error("the second tick fetched nothing at all, which means it did not run")
	}
}

// A model with no feed configured is skipped rather than erroring. A
// deployment ingesting only HRRR is an ordinary configuration.
func TestAModelWithNoFeedIsSkipped(t *testing.T) {
	t.Parallel()

	config := aPollConfig(0)
	config.Feeds = map[model.Model]string{model.HRRR: "hrrr"}

	poller, s, _ := pollHarness(t, config)

	if err := poller.Tick(t.Context()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	cycles, err := s.Cycles().List(t.Context(),
		store.CycleFilter{Model: model.GFS}, pkgstore.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(cycles) != 0 {
		t.Errorf("recorded %d GFS cycles with no GFS feed", len(cycles))
	}
}

func TestATickWithNoFeedsDoesNothing(t *testing.T) {
	t.Parallel()

	config := aPollConfig(0)
	config.Feeds = nil

	poller, s, _ := pollHarness(t, config)

	if err := poller.Tick(t.Context()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	cycles, err := s.Cycles().List(t.Context(), store.CycleFilter{}, pkgstore.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(cycles) != 0 {
		t.Errorf("recorded %d cycles with no feeds configured", len(cycles))
	}
}

// A canceled context stops the walk rather than finishing the horizon. A
// shutdown mid-backfill should not hold the process open for another twenty
// cycles.
func TestATickStopsOnCancellation(t *testing.T) {
	t.Parallel()

	poller, s, _ := pollHarness(t, aPollConfig(24*time.Hour))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := poller.Tick(ctx); err == nil {
		t.Fatal("a canceled tick reported success")
	}

	cycles, err := s.Cycles().List(t.Context(), store.CycleFilter{}, pkgstore.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(cycles) > 1 {
		t.Errorf("a canceled tick recorded %d cycles", len(cycles))
	}
}

func TestDefaultPollConfigIsUsable(t *testing.T) {
	t.Parallel()

	got := weather.DefaultPollConfig()

	if got.Interval <= 0 {
		t.Error("the default interval is not positive")
	}

	if got.Backfill != 24*time.Hour {
		t.Errorf("default backfill = %s, want 24h", got.Backfill)
	}

	// Disabled by default: which binary runs the loop is a property of the
	// deployment, not of this extension.
	if got.Enabled {
		t.Error("polling is enabled by default")
	}

	for _, hour := range got.Hours {
		if hour < 0 {
			t.Errorf("default hours contain %d", hour)
		}
	}
}

func TestAPollerNeedsAnEngine(t *testing.T) {
	t.Parallel()

	if _, err := weather.NewPoller(nil, aPollConfig(0), nil); err == nil {
		t.Fatal("NewPoller accepted a nil engine")
	}
}

// Every field the vocabulary knows is fetched when none is named, which is
// what the poller asks for.
func TestThePollerFetchesEveryField(t *testing.T) {
	t.Parallel()

	poller, s, _ := pollHarness(t, aPollConfig(0))

	if err := poller.Tick(t.Context()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	cycles, err := s.Cycles().List(t.Context(), store.CycleFilter{}, pkgstore.Page{})
	if err != nil || len(cycles) == 0 {
		t.Fatalf("List: %v", err)
	}

	grids, err := s.Grids().ForCycle(t.Context(), cycles[0].ID)
	if err != nil {
		t.Fatalf("ForCycle: %v", err)
	}

	// The synthetic sidecar carries three of the vocabulary's fields, so three
	// is what a full-vocabulary request can find in it.
	if len(grids) != 3 {
		t.Errorf("stored %d grids, want the 3 the sidecar carries", len(grids))
	}

	for _, grid := range grids {
		if !grid.Field.Valid() {
			t.Errorf("grid %s holds %q, which is outside the vocabulary",
				grid.ID, grid.Field)
		}
	}

	_ = field.Fields()
}
