package scenario

// The queue drainer's lifecycle. Split from extension.go because it is the
// only part of this extension that owns a goroutine, and because the logging
// below belongs to nothing else: Worker reports a tick failure by returning
// it, and this is the thing that hears.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xraph/forge"
)

// Asserted at compile time, for the reason api.go asserts
// ControllerWithMiddleware. Forge discovers both by type assertion and says
// nothing when one fails: a controller that stops satisfying its interface
// registers every route unguarded, and an extension that stops satisfying
// this one simply never starts its worker. Nothing queued would ever be
// claimed, and the only symptom would be runs sitting at "queued" forever.
var _ forge.RunnableExtension = (*Extension)(nil)

// Run starts the queue drainer. It satisfies forge.RunnableExtension, so Forge
// calls it after the HTTP server starts and cancels it on shutdown.
//
// # Why there is no leader election here
//
// hazard's loop is elected through a Postgres advisory lock, because two
// replicas rating one cycle would each write a computation row and leave two
// provenance runs for one forecast. This loop needs none: RunStore.Claim is an
// atomic hand-off — the memory store under one lock, the Fabriq store under a
// row lock and an expected-version check inside one transaction — so two
// workers can never receive the same run. Electing a leader here would take a
// queue that N replicas can safely share and serialize it onto one, which is
// throughput given away to prevent something that cannot happen.
func (e *Extension) Run(ctx context.Context) error {
	if !e.runWorker {
		return nil
	}

	if e.engine == nil {
		return errors.New("scenario: cannot start the worker before Start")
	}

	worker, err := NewWorker(e.engine, 0)
	if err != nil {
		return err
	}

	// WithoutCancel, because the context Forge hands a RunnableExtension is
	// the start-up one: it is canceled when startup finishes, and a loop tied
	// to it would stop the moment the application finished coming up.
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))

	e.mu.Lock()
	e.cancel = cancel
	e.done = make(chan struct{})
	done := e.done
	e.mu.Unlock()

	go func() {
		defer close(done)

		e.loop(runCtx, worker)
	}()

	return nil
}

// loop ticks until the context is canceled.
//
// The logging lives here rather than in Worker because Worker holds no logger
// — Tick reports a failure by returning it, and something has to be the thing
// that hears. A tick that fails is one run not executed, not a run lost: the
// claim it could not finish is still in the store, and the next tick finds
// whatever is queued.
func (e *Extension) loop(ctx context.Context, worker *Worker) {
	ticker := time.NewTicker(worker.Interval())
	defer ticker.Stop()

	// The first tick happens immediately rather than after an interval. A
	// process that just started should drain whatever queued while it was
	// down, not wait for the next period.
	for {
		if err := worker.Tick(ctx); err != nil && ctx.Err() == nil {
			if logger := e.Logger(); logger != nil {
				logger.Error("scenario: worker tick failed",
					forge.F("error", err.Error()))
			}
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
		return fmt.Errorf("scenario: the worker did not stop in time: %w", ctx.Err())
	}
}
