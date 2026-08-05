package scenario

import (
	"context"
	"testing"
	"time"

	scn "github.com/xraph/blackstart/extensions/scenario/scenario"
)

// queued enqueues one executable scenario against e and returns the run it
// produced: queued and unclaimed, the state a worker finds it in.
func queued(t *testing.T, e *Engine, f *fakes) scn.Run {
	t.Helper()

	manifest := executable(t, f)

	defined, err := e.Define(t.Context(), scn.Scenario{Name: "derecho", Manifest: manifest})
	if err != nil {
		t.Fatalf("define: %v", err)
	}

	run, err := e.Enqueue(t.Context(), defined.ID)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	return run
}

// aWorker builds a worker over e. The interval is irrelevant to every test
// here — Tick does not read it — so one fixed value stands in everywhere.
func aWorker(t *testing.T, e *Engine) *Worker {
	t.Helper()

	w, err := NewWorker(e, time.Minute)
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}

	return w
}

// An idle queue is not an error. Most ticks, in a running deployment, find
// nothing to do, and a worker that treated that as noteworthy would be noisy
// for no reason.
func TestATickWithNoQueuedRunsDoesNothing(t *testing.T) {
	t.Parallel()

	e, _ := engineWithFakes(t)

	if err := aWorker(t, e).Tick(t.Context()); err != nil {
		t.Fatalf("Tick on an empty queue: %v", err)
	}
}

// The ordinary case: one queued run, claimed and executed to an answer.
func TestATickExecutesAQueuedRunToCompletion(t *testing.T) {
	t.Parallel()

	f := working()
	e, store := engineWith(t, f)
	run := queued(t, e, f)

	if err := aWorker(t, e).Tick(t.Context()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	stored, err := store.Runs().Get(t.Context(), run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}

	if stored.Status != scn.StatusComplete {
		t.Fatalf("status = %q (%+v), want complete", stored.Status, stored.Failure)
	}
}

// A run that fails partway through the pipeline is left failed, with its
// step recorded, and the tick that ran it still returns nil: one bad run
// must not stop the worker. This is also the test that would catch Tick
// mistaking Execute's "recorded" for "succeeded" — a Tick that branched on
// the returned run's Status and only forwarded a nil error for
// StatusComplete would fail this the same way it would mislead an operator:
// by reporting a failed run as done.
func TestATickLeavesAFailingRunFailedAndReturnsNil(t *testing.T) {
	t.Parallel()

	f := working()
	f.hazard.err = errBroken
	e, store := engineWith(t, f)
	run := queued(t, e, f)

	if err := aWorker(t, e).Tick(t.Context()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	stored, err := store.Runs().Get(t.Context(), run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}

	if stored.Status != scn.StatusFailed {
		t.Fatalf("status = %q, want failed", stored.Status)
	}

	if stored.Failure == nil || stored.Failure.Step != scn.StepRatings {
		t.Fatalf("failure = %+v, want one naming the rating step", stored.Failure)
	}
}

// A second tick against a drained queue claims nothing and executes nothing
// a second time. Claim's own atomicity is what makes this true across two
// workers; this is the same property from one worker calling Tick twice.
func TestASecondTickDoesNotExecuteTheSameRunAgain(t *testing.T) {
	t.Parallel()

	f := working()
	e, store := engineWith(t, f)
	run := queued(t, e, f)

	w := aWorker(t, e)

	if err := w.Tick(t.Context()); err != nil {
		t.Fatalf("first tick: %v", err)
	}

	if err := w.Tick(t.Context()); err != nil {
		t.Fatalf("second tick: %v", err)
	}

	if f.screener.calls != 1 || f.simulator.calls != 1 {
		t.Errorf("two ticks over one run screened %d times and cascaded %d times, want 1 each",
			f.screener.calls, f.simulator.calls)
	}

	stored, err := store.Runs().Get(t.Context(), run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}

	if stored.Status != scn.StatusComplete {
		t.Fatalf("status = %q, want complete", stored.Status)
	}
}

// A canceled context stops Tick before it claims, not after. Claiming
// stamps a run running and hands it to a call that cannot go on to execute
// it — stranding it exactly where a crashed worker would leave one: claimed,
// unfinished, and invisible to every other worker until a reaper notices.
func TestATickOnACanceledContextClaimsNothing(t *testing.T) {
	t.Parallel()

	f := working()
	e, store := engineWith(t, f)
	run := queued(t, e, f)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := aWorker(t, e).Tick(ctx); err == nil {
		t.Fatal("want an error: the context was already canceled")
	}

	stored, err := store.Runs().Get(t.Context(), run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}

	if stored.Status != scn.StatusQueued {
		t.Errorf("status = %q, want the run still queued — a canceled tick must not "+
			"claim work it cannot finish", stored.Status)
	}
}

func TestNewWorkerNeedsAnEngine(t *testing.T) {
	t.Parallel()

	if _, err := NewWorker(nil, time.Minute); err == nil {
		t.Fatal("a worker was built with no engine")
	}
}

// Zero is not an instruction to spin; it falls back to a usable default, the
// same rule hazard's ComputeConfig applies.
func TestNewWorkerFallsBackToADefaultInterval(t *testing.T) {
	t.Parallel()

	e, _ := engineWithFakes(t)

	w, err := NewWorker(e, 0)
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}

	if w.Interval() != DefaultWorkerInterval {
		t.Errorf("interval = %v, want the default %v", w.Interval(), DefaultWorkerInterval)
	}
}
