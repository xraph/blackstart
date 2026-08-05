package scenario

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xraph/blackstart/pkg/bserr"
)

// DefaultWorkerInterval is the schedule a worker starts with when none is
// given.
//
// Short, for the same reason hazard's recompute interval is: the queue is
// durable state that a worker either finds work in or does not, so the cost
// of asking often is one Claim query rather than a network request. It is
// not tied to any other extension's poll period — this loop's only input is
// its own store.
const DefaultWorkerInterval = 5 * time.Second

// Worker drains the scenario run queue.
//
// One claimed run per Tick, deliberately — the same shape as
// extensions/hazard/worker.go's Computer, minus the sweep: hazard rates every
// sampled cycle it can reach in a tick because a cycle sits inert until
// something rates it, while a scenario run is claimed the moment a worker
// takes it, so there is nothing waiting behind it for this same tick to find.
// A caller that wants more throughput calls Tick more often, or runs more
// than one Worker; two of them can never receive the same run, because
// RunStore.Claim owns that atomically.
type Worker struct {
	engine   *Engine
	interval time.Duration
}

// NewWorker builds the worker.
//
// interval is not read by Tick — it is schedule information for whatever
// loop calls Tick on a period, kept on the worker rather than duplicated by
// its caller so the two cannot drift apart. A value that is zero or negative
// is not an instruction to spin; it is replaced with DefaultWorkerInterval,
// the same rule hazard's ComputeConfig applies to its own Interval.
func NewWorker(engine *Engine, interval time.Duration) (*Worker, error) {
	if engine == nil {
		return nil, errors.New("scenario: the worker needs an engine")
	}

	if interval <= 0 {
		interval = DefaultWorkerInterval
	}

	return &Worker{engine: engine, interval: interval}, nil
}

// Interval is how often a caller should invoke Tick.
func (w *Worker) Interval() time.Duration {
	return w.interval
}

// Tick claims one queued run and executes it.
//
// Claiming is Task 2's job, and it is already atomic there: RunStore.Claim
// takes the oldest queued run and marks it running under its own lock, in
// memory and against real Postgres, so two workers ticking at once can never
// receive the same run. Tick adds no locking, leasing or status re-check of
// its own — a second guard here would be a second place that decides who
// owns a run, and the two could disagree.
//
// An empty queue answers as bserr.ErrNotFound, which is the ordinary idle
// state of a worker with nothing to do. It is not logged or turned into an
// error: a deployment with no runs queued for most of its life is normal,
// and a Tick that treated that as noteworthy would be noisy for no reason.
//
// # Why a failed run does not make Tick return an error
//
// Execute's contract is what keeps this function from getting that wrong:
// a nil error means the outcome was RECORDED, not that it succeeded. A run
// that failed partway through the pipeline comes back with
// Status == StatusFailed, its Failure naming the step, and a nil error —
// that is Execute doing exactly its job, and the failed run is already a
// citable fact in the store by the time Tick sees it. Tick does not
// re-inspect the returned run's Status to decide what to report, because
// there is nothing left to decide: reading Status here and treating it as
// this function's own success/failure signal would be the second place that
// draws the same conclusion Execute already recorded, and the one place that
// could draw it wrong. Only a non-nil error — the outcome could not even be
// recorded — is this Tick's problem, because that is the one case nothing
// else already turned into a stored fact.
func (w *Worker) Tick(ctx context.Context) error {
	// Checked before Claim, not just before Execute. Claim is a hand-off, not
	// a lookup — it marks a run running and stamps its start time — so
	// claiming work this call has no chance to finish would strand that run
	// exactly where a crashed worker leaves one: running, unfinished, and
	// invisible to every other worker until something reaps it. Refusing
	// before the claim avoids manufacturing that state on purpose.
	if err := ctx.Err(); err != nil {
		return err
	}

	claimed, err := w.engine.store.Runs().Claim(ctx)
	if err != nil {
		if errors.Is(err, bserr.ErrNotFound) {
			return nil
		}

		return fmt.Errorf("scenario: claiming a run: %w", err)
	}

	if _, err := w.engine.Execute(ctx, claimed); err != nil {
		return fmt.Errorf("scenario: executing run %s: %w", claimed.ID, err)
	}

	return nil
}
