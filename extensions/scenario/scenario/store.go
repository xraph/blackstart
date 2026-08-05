package scenario

import (
	"context"

	"github.com/xraph/blackstart/pkg/id"
)

// Store persists scenario bindings.
//
// A second interface here rather than a second package, unlike contingency's
// sweep.Store and cascade's run.Store: Scenario and Run are both declared in
// this one package already, so splitting the store interfaces across
// packages would draw a boundary the types themselves do not have. One
// interface per domain type is still the rule this file follows — it just
// does not need a directory of its own to do it.
type Store interface {
	// Create records a scenario.
	Create(ctx context.Context, in Scenario) (Scenario, error)

	// Get retrieves one scenario by identifier.
	Get(ctx context.Context, scenarioID id.ID) (Scenario, error)
}

// RunStore persists scenario runs and carries them through their lifecycle.
type RunStore interface {
	// Create records a run. Callers are expected to hand in a run with
	// Status StatusQueued — Create does not decide that for them, the same
	// way sweep.Store and run.Store do not decide a run's terminal state.
	Create(ctx context.Context, in Run) (Run, error)

	// Get retrieves one run by identifier.
	Get(ctx context.Context, runID id.ID) (Run, error)

	// ListByScenario returns runs for one scenario, most recent first.
	//
	// A limit of zero or less returns everything, which is what a CLI wants.
	ListByScenario(ctx context.Context, scenarioID id.ID, limit int) ([]Run, error)

	// Claim takes the oldest queued run, marks it running, and returns it.
	//
	// This is the one place the scenario extension has concurrency: several
	// workers may call Claim at once, and it must be atomic under the
	// store's own lock so that two of them never receive the same run — a
	// scenario simulated twice by two workers that each believe they own it
	// is exactly the failure mode a queue exists to prevent.
	//
	// Returns an error satisfying errors.Is(err, bserr.ErrNotFound) when
	// nothing is queued, never a zero Run. A zero Run has an empty Status,
	// which would flow into a worker indistinguishable from a real one.
	Claim(ctx context.Context) (Run, error)

	// Finish records a run's terminal state.
	//
	// The other half of Claim, and the only way out of running: Create
	// records a queued run and Claim moves it to running, so without this
	// every execution would end with its answer nowhere and its run still
	// reading as in progress.
	//
	// The run handed in must be StatusComplete or StatusFailed, and the run
	// it names must currently be running. Both are refused rather than
	// accommodated. A Finish that accepted StatusQueued would un-run a run
	// that had already produced an answer, and one that accepted a run
	// nobody had claimed would let two workers each record their own result
	// for the same scenario — which is precisely what Claim exists to
	// prevent, and would make the queue's atomicity decorative.
	//
	// Returns an error satisfying errors.Is(err, bserr.ErrNotFound) for a run
	// that does not exist, and one satisfying errors.Is(err,
	// bserr.ErrConflict) for a run that is not running.
	Finish(ctx context.Context, in Run) (Run, error)
}
