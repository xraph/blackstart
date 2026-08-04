package weather

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/fabriq/adapters/postgres"

	"github.com/xraph/forge"

	"github.com/xraph/blackstart/extensions/fabric"
	"github.com/xraph/blackstart/extensions/weather/fetch"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	"github.com/xraph/blackstart/extensions/weather/model"
	"github.com/xraph/blackstart/extensions/weather/store"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

// The polling worker.
//
// Leader-elected through a PostgreSQL advisory lock so that N replicas
// cooperate rather than each fetching the same cycle. The key is namespaced
// away from fabriq's own (1001-1004) and from the fabric relay's (20001), so a
// weather worker and a relay in one database do not contend for the same lock
// and silently disable one another.
const lockKeyWeatherPoll int64 = 20002

// PollConfig is how the worker behaves.
type PollConfig struct {
	// Enabled starts the loop. The server runs it; bsctl does not.
	Enabled bool

	// Interval is how often to look for new cycles.
	//
	// HRRR publishes hourly but not on the hour — a run takes most of an hour
	// to appear — so polling considerably more often than the cadence is what
	// keeps the delay between publication and ingest short. An unpublished
	// cycle costs one request that answers 404.
	Interval time.Duration

	// Backfill is how far back to walk filling gaps.
	//
	// Bounded, and configuration rather than a constant. Operational
	// correctness comes first — the newest cycle is ingested before any
	// backfill — but a missed cycle cannot be recovered once NOAA's rolling
	// archive passes it, and Phase 6's backtest cannot reconstruct what was
	// knowable at a time whose forecast was never stored. A backtest-focused
	// deployment widens this; a demo sets it to zero.
	Backfill time.Duration

	// SourceSlug and Feeds name what to poll: the registry source and, per
	// model, the feed within it.
	SourceSlug string
	Feeds      map[model.Model]string

	// Hours are the forecast hours to retrieve per cycle.
	Hours []int
}

// DefaultPollConfig is the shape a server starts with.
func DefaultPollConfig() PollConfig {
	return PollConfig{
		Interval: 10 * time.Minute,
		Backfill: 24 * time.Hour,
		Hours:    []int{0, 1, 2, 3, 6, 12, 18},
	}
}

// Poller drives ingest on a schedule.
type Poller struct {
	engine *Engine
	config PollConfig
	logger forge.Logger

	now func() time.Time
}

// NewPoller builds the loop.
func NewPoller(engine *Engine, config PollConfig, logger forge.Logger) (*Poller, error) {
	if engine == nil {
		return nil, errors.New("weather: the poller needs an engine")
	}

	if config.Interval <= 0 {
		config.Interval = DefaultPollConfig().Interval
	}

	return &Poller{
		engine: engine,
		config: config,
		logger: logger,
		now:    time.Now,
	}, nil
}

// SetClock replaces the time source.
//
// Exported because the poller's whole behavior is a function of "now" — which
// cycles exist, how far back the horizon reaches — and a test that could not
// pin it would have to sleep or assert against a moving target.
func (p *Poller) SetClock(now func() time.Time) {
	if now != nil {
		p.now = now
	}
}

// Tick runs one pass: retrieve, then sample what has been retrieved.
//
// Retrieval takes the newest cycle first, then walks backwards to the horizon.
// Newest first because operational correctness is the more urgent of the two
// goals — a current forecast matters now, and a six-hour-old one does not.
// Backwards afterwards because a missed cycle is unrecoverable once the
// archive rolls past it.
//
// Sampling is a separate sweep over whatever is in the fetched state rather
// than something chained onto each retrieval, and the difference matters when
// the grid service is down: retrieval still succeeds, the bytes are safe, and
// the next tick samples them once the solver is back. Chaining would lose the
// cycle to a transient outage in a component retrieval does not need.
//
// Idempotent throughout, which is what makes this safe on every restart: a
// cycle already recorded costs one lookup and reports as skipped.
func (p *Poller) Tick(ctx context.Context) error {
	if err := p.retrieve(ctx); err != nil {
		return err
	}

	return p.sample(ctx)
}

// retrieve is the ingest half of a tick.
func (p *Poller) retrieve(ctx context.Context) error {
	if len(p.config.Feeds) == 0 {
		return nil
	}

	now := p.now().UTC()
	from := now.Add(-p.config.Backfill)

	// Sorted so two models are polled in a fixed order. Map iteration would
	// make a partial tick — one interrupted by shutdown — depend on which
	// model happened to come first.
	for _, m := range model.All() {
		feedSlug, ok := p.config.Feeds[m]
		if !ok {
			continue
		}

		cycles, err := fetch.Cycles(m, from, now)
		if err != nil {
			return fmt.Errorf("weather: enumerating %s cycles: %w", m, err)
		}

		for _, issuedAt := range cycles {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			p.ingestOne(ctx, m, feedSlug, issuedAt)
		}
	}

	return nil
}

// maxSampleSweep bounds how many fetched cycles one tick will sample.
//
// A bound rather than everything, because the first tick after a long outage
// could otherwise hold the leader lock for as long as the backlog takes and
// starve retrieval — which is the half that cannot be recovered later. The
// remainder is picked up next tick.
const maxSampleSweep = 8

// sample turns retrieved grids into per-span samples.
//
// It sweeps the fetched state rather than sampling what this tick just
// retrieved, so a cycle that arrived while the grid service was down is picked
// up as soon as it comes back, and a cycle ingested by `bsctl` is sampled by
// the worker without anyone asking.
func (p *Poller) sample(ctx context.Context) error {
	pending, err := p.engine.Cycles(ctx,
		store.CycleFilter{Status: forecast.StatusFetched},
		pkgstore.Page{Limit: maxSampleSweep})
	if err != nil {
		return fmt.Errorf("weather: listing cycles awaiting sampling: %w", err)
	}

	for _, cycle := range pending {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		p.sampleOne(ctx, cycle)
	}

	return nil
}

// sampleOne samples a single cycle, logging rather than failing the tick.
//
// The same reasoning as ingestOne: one cycle the grid service will not decode
// should not stop the sweep, and the cycle stays in the fetched state so the
// next tick tries again.
func (p *Poller) sampleOne(ctx context.Context, cycle forecast.Cycle) {
	result, err := p.engine.SampleCycle(ctx, cycle.ID, nil)
	if err != nil {
		p.log("weather: sampling failed",
			forge.F("cycle", cycle.ID.String()),
			forge.F("model", string(cycle.Model)),
			forge.F("issued_at", cycle.IssuedAt.Format(time.RFC3339)),
			forge.F("error", err.Error()))

		return
	}

	p.log("weather: sampled",
		forge.F("cycle", cycle.ID.String()),
		forge.F("model", string(cycle.Model)),
		forge.F("issued_at", cycle.IssuedAt.Format(time.RFC3339)),
		forge.F("grids", result.Grids),
		forge.F("samples", result.Samples),
		forge.F("covered", result.Covered),
		forge.F("total", result.Total))
}

// ingestOne retrieves a single cycle, logging rather than failing the tick.
//
// One cycle that will not fetch should not stop the pass: the next one down
// may be perfectly available, and a worker that gave up on the first failure
// would never reach the backfill at all.
func (p *Poller) ingestOne(
	ctx context.Context, m model.Model, feedSlug string, issuedAt time.Time,
) {
	result, err := p.engine.Ingest(ctx, IngestRequest{
		SourceSlug: p.config.SourceSlug,
		FeedSlug:   feedSlug,
		Model:      m,
		IssuedAt:   issuedAt,
		Hours:      p.config.Hours,
	})
	if err != nil {
		p.log("weather: ingest failed",
			forge.F("model", string(m)),
			forge.F("issued_at", issuedAt.Format(time.RFC3339)),
			forge.F("error", err.Error()))

		return
	}

	// A pass that only skipped is the ordinary state of everything but the
	// newest cycle, and logging it every ten minutes would bury the rest.
	if result.Retrieved == 0 && result.Failed == 0 {
		return
	}

	p.log("weather: ingested",
		forge.F("model", string(m)),
		forge.F("issued_at", issuedAt.Format(time.RFC3339)),
		forge.F("retrieved", result.Retrieved),
		forge.F("skipped", result.Skipped),
		forge.F("absent", result.Absent),
		forge.F("failed", result.Failed))
}

func (p *Poller) log(message string, fields ...forge.Field) {
	if p.logger == nil {
		return
	}

	p.logger.Info(message, fields...)
}

// Run starts the polling loop. It satisfies forge.RunnableExtension, so Forge
// calls it after the HTTP server starts and cancels it on shutdown.
func (e *Extension) Run(ctx context.Context) error {
	if !e.poll.Enabled {
		return nil
	}

	if e.engine == nil {
		return errors.New("weather: cannot start the poller before Start")
	}

	poller, err := NewPoller(e.engine, e.poll, e.Logger())
	if err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))

	e.mu.Lock()
	e.cancel = cancel
	e.done = make(chan struct{})
	done := e.done
	e.mu.Unlock()

	var wg sync.WaitGroup

	// Leader-elected when a Postgres handle is available, which it is whenever
	// the fabric extension started. N replicas each polling NOAA for the same
	// cycle would multiply the request rate by the replica count for no gain,
	// and public endpoints are exactly where that gets a deployment
	// rate-limited.
	//
	// Without a handle — a memory-store deployment, or a test — the loop runs
	// unelected. That is correct rather than a fallback: there is no second
	// replica to contend with.
	if elector := e.elector(); elector != nil {
		wg.Go(func() {
			if err := elector.Run(runCtx, poller.loop2); err != nil &&
				runCtx.Err() == nil {
				e.Logger().Error("weather: leader election failed",
					forge.F("error", err.Error()))
			}
		})
	} else {
		wg.Go(func() { poller.loop(runCtx) })
	}

	go func() {
		wg.Wait()
		close(done)
	}()

	return nil
}

// loop2 is loop in the shape the elector wants: it runs while this replica
// leads, and returning abdicates.
func (p *Poller) loop2(ctx context.Context) error {
	p.loop(ctx)

	return ctx.Err()
}

// elector builds the leader election, or nil when there is nothing to elect
// against.
func (e *Extension) elector() *postgres.Elector {
	app := e.App()
	if app == nil {
		return nil
	}

	fabricExt, err := forge.Inject[*fabric.Extension](app.Container())
	if err != nil {
		return nil
	}

	stores := fabricExt.Stores()
	if stores == nil || stores.Postgres == nil {
		return nil
	}

	return postgres.NewElector(stores.Postgres, lockKeyWeatherPoll)
}

// loop ticks until the context is canceled.
func (p *Poller) loop(ctx context.Context) {
	ticker := time.NewTicker(p.config.Interval)
	defer ticker.Stop()

	// The first tick happens immediately rather than after an interval. A
	// deployment that just started should not wait ten minutes to notice the
	// cycle it missed while it was down.
	for {
		if err := p.Tick(ctx); err != nil && ctx.Err() == nil {
			p.log("weather: poll tick failed", forge.F("error", err.Error()))
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Shutdown stops the loop and waits for it.
func (e *Extension) Shutdown(ctx context.Context) error {
	e.mu.Lock()
	cancel, done := e.cancel, e.done
	e.cancel, e.done = nil, nil
	e.mu.Unlock()

	if cancel == nil {
		return nil
	}

	cancel()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("weather: the poller did not stop in time: %w", ctx.Err())
	}
}
