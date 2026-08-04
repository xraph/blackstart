package hazard

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/fabriq/adapters/postgres"

	"github.com/xraph/forge"

	"github.com/xraph/blackstart/extensions/fabric"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	wxstore "github.com/xraph/blackstart/extensions/weather/store"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

// The recompute worker.
//
// Leader-elected through a PostgreSQL advisory lock so that N replicas
// cooperate rather than each rating the same cycle. The key is namespaced away
// from fabriq's own (1001-1004), the fabric relay's (20001) and the weather
// poller's (20002); two workers sharing a key would silently disable one
// another.
const lockKeyHazardCompute int64 = 20003

// ComputeConfig is how the worker behaves.
type ComputeConfig struct {
	// Enabled starts the loop. The server runs it; bsctl does not.
	Enabled bool

	// Interval is how often to look for cycles that have been sampled and not
	// yet rated.
	//
	// Shorter than the weather poller's, because this loop makes no network
	// requests: it reads two tables and either finds work or does not. The
	// cost of asking often is a query, and the benefit is that a rating
	// follows its forecast by minutes rather than by a poll cycle.
	Interval time.Duration

	// Lookback bounds how far back to consider cycles.
	//
	// A cycle older than this is left alone even if it was never rated. The
	// bound exists because an unrated cycle from last week is not an
	// operational problem — nothing is deciding anything from it — and sweeping
	// the whole history on every tick would make the query grow without limit.
	Lookback time.Duration
}

// DefaultComputeConfig is the shape a server starts with.
func DefaultComputeConfig() ComputeConfig {
	return ComputeConfig{
		Interval: 2 * time.Minute,
		Lookback: 12 * time.Hour,
	}
}

// maxComputeSweep bounds how many cycles one tick will rate.
//
// A pass over a large network is the expensive thing this extension does, so a
// backlog is worked through a few cycles at a time rather than in one tick
// that holds the leader lock for minutes.
const maxComputeSweep = 4

// Computer rates sampled cycles on a schedule.
type Computer struct {
	engine *Engine
	config ComputeConfig
	logger forge.Logger

	now func() time.Time
}

// NewComputer builds the loop.
func NewComputer(
	engine *Engine, config ComputeConfig, logger forge.Logger,
) (*Computer, error) {
	if engine == nil {
		return nil, errors.New("hazard: the computer needs an engine")
	}

	if config.Interval <= 0 {
		config.Interval = DefaultComputeConfig().Interval
	}

	if config.Lookback <= 0 {
		config.Lookback = DefaultComputeConfig().Lookback
	}

	return &Computer{
		engine: engine,
		config: config,
		logger: logger,
		now:    time.Now,
	}, nil
}

// SetClock replaces the time source.
//
// Exported for the reason the weather poller's is: the lookback window is a
// function of "now", and a test that could not pin it would have to sleep or
// assert against a moving target.
func (c *Computer) SetClock(now func() time.Time) {
	if now != nil {
		c.now = now
	}
}

// Tick rates every sampled cycle inside the lookback that has no computation
// yet.
//
// The queue is durable state rather than a channel or an event: a cycle is
// pending exactly when it is sampled and has no computation row. That survives
// a restart, works across replicas, and cannot drop work the way an in-process
// notification can — a missed event would leave a forecast permanently
// unrated with nothing to show it had happened.
//
// A cycle that already has a computation is skipped, which means a
// recomputation after a conductor assignment changes is a deliberate act
// through `bsctl hazard compute` rather than something this loop does on its
// own. Ratings already consumed downstream should not move because a worker
// decided to try again.
func (c *Computer) Tick(ctx context.Context) error {
	if c.engine.weather == nil || c.engine.geometry == nil {
		return nil
	}

	since := c.now().UTC().Add(-c.config.Lookback)

	sampled, err := c.engine.weather.Cycles(ctx,
		wxstore.CycleFilter{Status: forecast.StatusSampled},
		pkgstore.Page{Limit: pkgstore.MaxPageLimit})
	if err != nil {
		return fmt.Errorf("hazard: listing sampled cycles: %w", err)
	}

	rated := 0

	for _, cycle := range sampled {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if rated >= maxComputeSweep {
			return nil
		}

		if cycle.IssuedAt.Before(since) {
			// The listing is newest first, so the first cycle past the
			// lookback ends the sweep.
			return nil
		}

		done, err := c.alreadyRated(ctx, cycle)
		if err != nil {
			return err
		}

		if done {
			continue
		}

		c.computeOne(ctx, cycle)

		rated++
	}

	return nil
}

// alreadyRated reports whether a cycle has been through the models.
func (c *Computer) alreadyRated(
	ctx context.Context, cycle forecast.Cycle,
) (bool, error) {
	existing, err := c.engine.ComputationsForCycle(ctx, cycle.ID)
	if err != nil {
		return false, fmt.Errorf(
			"hazard: reading computations of cycle %s: %w", cycle.ID, err)
	}

	return len(existing) > 0, nil
}

// computeOne rates a single cycle, logging rather than failing the tick.
//
// One cycle that will not rate should not stop the sweep: the next one may be
// fine, and a worker that gave up on the first failure would stall on a single
// bad cycle forever.
func (c *Computer) computeOne(ctx context.Context, cycle forecast.Cycle) {
	result, err := c.engine.Compute(ctx, ComputeRequest{CycleID: cycle.ID})
	if err != nil {
		c.log("hazard: computation failed",
			forge.F("cycle", cycle.ID.String()),
			forge.F("issued_at", cycle.IssuedAt.Format(time.RFC3339)),
			forge.F("error", err.Error()))

		return
	}

	c.log("hazard: computed",
		forge.F("cycle", cycle.ID.String()),
		forge.F("computation", result.ComputationID.String()),
		forge.F("issued_at", cycle.IssuedAt.Format(time.RFC3339)),
		forge.F("spans", result.Spans),
		forge.F("unrated", result.Unrated),
		forge.F("values", result.Values))

	// Worth its own line at warning level rather than a field on the line
	// above. A network where most spans have no conductor produces ratings
	// that look complete until somebody counts them.
	if result.Unrated > 0 && c.logger != nil {
		c.logger.Warn("hazard: spans skipped for want of a conductor assignment",
			forge.F("cycle", cycle.ID.String()),
			forge.F("unrated", result.Unrated),
			forge.F("rated", result.Spans))
	}
}

func (c *Computer) log(message string, fields ...forge.Field) {
	if c.logger == nil {
		return
	}

	c.logger.Info(message, fields...)
}

// Run starts the recompute loop. It satisfies forge.RunnableExtension, so
// Forge calls it after the HTTP server starts and cancels it on shutdown.
func (e *Extension) Run(ctx context.Context) error {
	if !e.compute.Enabled {
		return nil
	}

	if e.engine == nil {
		return errors.New("hazard: cannot start the computer before Start")
	}

	computer, err := NewComputer(e.engine, e.compute, e.Logger())
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

	// Leader-elected when a Postgres handle is available. Two replicas rating
	// the same cycle would both write a computation row and both write the
	// same values, leaving two provenance runs for one forecast with nothing
	// to say which a reader should believe.
	//
	// Without a handle — a memory-store deployment, or a test — the loop runs
	// unelected. That is correct rather than a fallback: there is no second
	// replica to contend with.
	if elector := e.elector(); elector != nil {
		wg.Go(func() {
			if err := elector.Run(runCtx, computer.lead); err != nil &&
				runCtx.Err() == nil {
				e.Logger().Error("hazard: leader election failed",
					forge.F("error", err.Error()))
			}
		})
	} else {
		wg.Go(func() { computer.loop(runCtx) })
	}

	go func() {
		wg.Wait()
		close(done)
	}()

	return nil
}

// lead is loop in the shape the elector wants: it runs while this replica
// leads, and returning abdicates.
func (c *Computer) lead(ctx context.Context) error {
	c.loop(ctx)

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

	return postgres.NewElector(stores.Postgres, lockKeyHazardCompute)
}

// loop ticks until the context is canceled.
func (c *Computer) loop(ctx context.Context) {
	ticker := time.NewTicker(c.config.Interval)
	defer ticker.Stop()

	// The first tick happens immediately rather than after an interval. A
	// deployment that just started should rate whatever it missed while it was
	// down, not wait for the next period.
	for {
		if err := c.Tick(ctx); err != nil && ctx.Err() == nil {
			c.log("hazard: compute tick failed", forge.F("error", err.Error()))
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
		return fmt.Errorf("hazard: the computer did not stop in time: %w", ctx.Err())
	}
}
