package scenario

import (
	"context"

	"github.com/xraph/blackstart/pkg/id"
)

// Store persists scenario bindings.
//
// A second interface here rather than a second package, unlike contingency's
// sweep.Store and cascade's run.Store: Scenario and ScenarioRun are both
// declared in this one package already, so splitting the store interfaces
// across packages would draw a boundary the types themselves do not have. One
// interface per domain type is still the rule this file follows — it just
// does not need a directory of its own to do it.
type Store interface {
	// Create records a scenario.
	Create(ctx context.Context, in Scenario) (Scenario, error)

	// Get retrieves one scenario by identifier.
	Get(ctx context.Context, scenarioID id.ID) (Scenario, error)

	// Update replaces a stored scenario.
	//
	// A scenario is editable — the design calls it named, editable, reusable —
	// and this is the only method that edits one. It cannot reach a run that
	// already exists: Engine.Enqueue freezes a deep copy of the manifest into
	// the run at the moment it is queued, and that is the whole reason a
	// stored run stays citable after the scenario it came from has moved on.
	//
	// Returns an error satisfying errors.Is(err, bserr.ErrNotFound) for a
	// scenario nothing has defined. An Update that created one would turn a
	// mistyped identifier into a second scenario nobody meant to define,
	// which is the same class of quiet wrong answer Create refuses when it
	// rejects a duplicate rather than overwriting.
	Update(ctx context.Context, in Scenario) (Scenario, error)

	// List returns scenarios, most recently created first.
	//
	// A limit of zero or less returns everything, the same rule
	// ListByScenario states and for the same reason: a caller that asked for
	// everything and silently got the newest fifty of sixty has no way to
	// tell it happened, so the bound belongs at the caller that knows what it
	// wants.
	List(ctx context.Context, limit int) ([]Scenario, error)
}

// RunStore persists scenario runs and carries them through their lifecycle.
type RunStore interface {
	// Create records a run. Callers are expected to hand in a run with
	// Status StatusQueued — Create does not decide that for them, the same
	// way sweep.Store and run.Store do not decide a run's terminal state.
	Create(ctx context.Context, in ScenarioRun) (ScenarioRun, error)

	// Get retrieves one run by identifier.
	Get(ctx context.Context, runID id.ID) (ScenarioRun, error)

	// ListByScenario returns runs for one scenario, most recent first.
	//
	// A limit of zero or less returns everything, which is what a CLI wants.
	ListByScenario(ctx context.Context, scenarioID id.ID, limit int) ([]ScenarioRun, error)

	// Claim takes the oldest queued run, marks it running, and returns it.
	//
	// This is the one place the scenario extension has concurrency: several
	// workers may call Claim at once, and it must be atomic under the
	// store's own lock so that two of them never receive the same run — a
	// scenario simulated twice by two workers that each believe they own it
	// is exactly the failure mode a queue exists to prevent.
	//
	// Returns an error satisfying errors.Is(err, bserr.ErrNotFound) when
	// nothing is queued, never a zero ScenarioRun. A zero run has an empty
	// Status, which would flow into a worker indistinguishable from a real one.
	Claim(ctx context.Context) (ScenarioRun, error)

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
	Finish(ctx context.Context, in ScenarioRun) (ScenarioRun, error)
}
