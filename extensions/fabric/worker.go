package fabric

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/fabriq/adapters/postgres"
	"github.com/xraph/forge"
)

// The outbox worker.
//
// Nothing in fabriq.Open starts this. Open assembles the stores and the
// facade; the relay that drains the outbox and the consumers that apply
// projections are separate loops, and Fabriq's own forgeext runs them in its
// Run method. Since blackstart cannot use forgeext — it swallows the lifecycle
// options this extension exists to pass — the worker is ours to run too.
//
// That was not obvious from the configuration. RunWorker existed and was
// validated against a Redis address for several commits before anything
// actually started, which meant a deployment could be configured correctly and
// still never advance a projection. The integration test asserting a write
// reaches the graph is what surfaced it, because it is the only check that
// fails when the outbox is never drained.
//
// Two loops, both leader-elected through PostgreSQL advisory locks so that N
// replicas cooperate rather than duplicating work:
//
//   - The relay reads the transactional outbox and publishes to Redis.
//   - The graph consumer reads Redis and applies mutations to FalkorDB.

// Advisory lock keys. They are namespaced away from Fabriq's own (1001-1004)
// so a blackstart worker and a Fabriq worker in the same database do not
// contend for the same lock and silently disable one another.
const (
	lockKeyRelay int64 = 20001
)

// Run starts the worker loops. It satisfies forge.RunnableExtension, so Forge
// calls it after the HTTP server starts and cancels it on shutdown.
func (e *Extension) Run(ctx context.Context) error {
	if !e.config.RunWorker {
		e.Logger().Info("fabric: worker disabled; something else must drain the outbox")

		return nil
	}

	stores := e.Stores()
	if stores == nil {
		return errors.New("fabric: cannot start the worker before Start")
	}

	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))

	e.mu.Lock()
	e.cancel = cancel
	e.done = make(chan struct{})
	done := e.done
	e.mu.Unlock()

	var wg sync.WaitGroup

	// The relay is the piece without which nothing else matters: an undrained
	// outbox grows without bound and no projection ever advances.
	relay := postgres.NewRelay(stores.Postgres, e.reg, stores.Redis)
	elector := postgres.NewElector(stores.Postgres, lockKeyRelay)

	wg.Go(func() {
		e.supervise(runCtx, "relay", func(c context.Context) error {
			return elector.Run(c, relay.Run)
		})
	})

	if stores.Falkor != nil {
		engine, err := stores.GraphEngine(e.reg, nil)
		if err != nil {
			cancel()

			return fmt.Errorf("fabric: assembling the graph consumer: %w", err)
		}

		// Consumer groups scale without election: each replica takes a share
		// of the stream rather than one leader taking all of it.
		wg.Go(func() {
			e.supervise(runCtx, "graph", func(c context.Context) error {
				return engine.Run(c, e.consumerName())
			})
		})
	}

	go func() {
		wg.Wait()
		close(done)
	}()

	e.Logger().Info("fabric: worker started",
		forge.F("relay", true),
		forge.F("graph", stores.Falkor != nil),
	)

	return nil
}

// Shutdown stops the worker loops and waits for them.
//
// Waiting matters: a relay killed mid-batch leaves its advisory lock held
// until the connection drops, and the next replica to try cannot take over
// until then.
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
		return fmt.Errorf("fabric: worker did not stop in time: %w", ctx.Err())
	}
}

// supervise restarts a loop that returns an error, backing off between
// attempts, until the context is canceled.
//
// A relay that stops on a transient database error and never restarts is
// indistinguishable from one that is working, right up until someone notices
// the outbox is millions of rows deep. Restarting is the only safe default.
func (e *Extension) supervise(ctx context.Context, label string, fn func(context.Context) error) {
	const backoff = 2 * time.Second

	for {
		if ctx.Err() != nil {
			return
		}

		err := fn(ctx)

		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			e.Logger().Error("fabric: worker loop failed, restarting",
				forge.F("loop", label),
				forge.F("error", err.Error()),
			)
		default:
			e.Logger().Warn("fabric: worker loop returned, restarting",
				forge.F("loop", label))
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

// consumerName identifies this replica within the projection consumer group.
func (e *Extension) consumerName() string {
	return "blackstart-" + e.config.Tenant
}
