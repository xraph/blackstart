package scenario

import (
	"context"
	"errors"
	"fmt"

	scnid "github.com/xraph/blackstart/extensions/scenario/id"
	scn "github.com/xraph/blackstart/extensions/scenario/scenario"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/hooks"
	"github.com/xraph/blackstart/pkg/id"
)

// Define records a scenario, or replaces one that already exists.
//
// An upsert rather than a create, because a scenario is a definition someone
// authors and revises. Revising one is safe precisely because it cannot reach
// a run: Enqueue freezes a copy of the manifest, so an edit changes what the
// NEXT run will execute and nothing about any run already recorded.
//
// CreatedAt survives an edit and UpdatedAt does not. A scenario that was
// renamed is the same scenario, and moving its creation time forward would
// erase the only evidence of how long the binding has existed.
func (e *Engine) Define(ctx context.Context, in scn.Scenario) (scn.Scenario, error) {
	// Normalized on the way in, the way both stores already normalize every
	// row timestamp. ValidAt is the one instant a caller supplies, and it is
	// the field the manifest's identity rests on: the Fabriq store round-trips
	// it through a JSON column, which renders a named zone as a fixed offset,
	// so the same scenario defined at 14:00 America/New_York comes back from
	// one store carrying a zone the other's copy does not. Nothing at runtime
	// notices — the rating lookup compares with Equal, which is
	// zone-independent — but it is the last behavioral difference between the
	// two stores, and reflect.DeepEqual is how store parity gets checked.
	in.Manifest.ValidAt = in.Manifest.ValidAt.UTC()

	if err := in.Manifest.Validate(); err != nil {
		return scn.Scenario{}, err
	}

	now := e.now().UTC()

	if in.ID.IsZero() {
		in.ID = scnid.NewScenario()
	}

	existing, err := e.store.Scenarios().Get(ctx, in.ID)
	if err != nil {
		if !errors.Is(err, bserr.ErrNotFound) {
			return scn.Scenario{}, err
		}

		in.CreatedAt, in.UpdatedAt = now, now

		return e.store.Scenarios().Create(ctx, in)
	}

	in.CreatedAt = existing.CreatedAt
	in.UpdatedAt = now

	return e.store.Scenarios().Update(ctx, in)
}

// Get retrieves one scenario.
func (e *Engine) Get(ctx context.Context, scenarioID id.ID) (scn.Scenario, error) {
	return e.store.Scenarios().Get(ctx, scenarioID)
}

// List returns scenarios, most recently created first.
func (e *Engine) List(ctx context.Context, limit int) ([]scn.Scenario, error) {
	return e.store.Scenarios().List(ctx, limit)
}

// Run retrieves one run.
func (e *Engine) Run(ctx context.Context, runID id.ID) (scn.ScenarioRun, error) {
	return e.store.Runs().Get(ctx, runID)
}

// Runs returns one scenario's runs, most recent first.
func (e *Engine) Runs(ctx context.Context, scenarioID id.ID, limit int) ([]scn.ScenarioRun, error) {
	return e.store.Runs().ListByScenario(ctx, scenarioID, limit)
}

// RecentRuns returns runs across every scenario, most recent first.
func (e *Engine) RecentRuns(ctx context.Context, limit int) ([]scn.ScenarioRun, error) {
	return e.store.Runs().List(ctx, limit)
}

// Enqueue freezes a scenario's manifest into a new queued run.
//
// This is the moment the reproducibility contract is made, and the reason
// enqueueing is separate from executing. The run carries its own copy of the
// manifest from here on and Execute never reads the scenario again — so a
// stored run describes the inputs it was computed from even after the
// scenario it came from has been edited, and "same manifest, same result" is
// a claim someone can check rather than one they have to trust.
//
// Clone rather than assignment. A Manifest is a value type and every scalar
// it holds copies by assignment, which makes a plain copy look total;
// Branches is a slice, and a copied struct shares its backing array with the
// scenario's.
func (e *Engine) Enqueue(ctx context.Context, scenarioID id.ID) (scn.ScenarioRun, error) {
	found, err := e.store.Scenarios().Get(ctx, scenarioID)
	if err != nil {
		return scn.ScenarioRun{}, err
	}

	stored, err := e.store.Runs().Create(ctx, scn.ScenarioRun{
		ID:         scnid.NewRun(),
		ScenarioID: found.ID,
		Manifest:   found.Manifest.Clone(),
		Status:     scn.StatusQueued,
		QueuedAt:   e.now().UTC(),
	})
	if err != nil {
		return scn.ScenarioRun{}, fmt.Errorf("scenario: queueing a run: %w", err)
	}

	e.emit(ctx, stored)

	return stored, nil
}

// emit publishes a run's current state, best effort and out of transaction: a
// subscriber that fails must not undo a run that was recorded.
//
// Runs only. A scenario definition arrives through an HTTP request whose
// caller already knows it happened; a run's transitions happen inside a
// worker, where an event is the only way anything else learns of them.
func (e *Engine) emit(ctx context.Context, run scn.ScenarioRun) {
	if e.bus == nil {
		return
	}

	// A queued run is the record coming into existence; every later event on
	// it is that record changing.
	op, at := hooks.OpUpdate, run.FinishedAt
	if run.Status == scn.StatusQueued {
		op, at = hooks.OpCreate, run.QueuedAt
	}

	e.bus.Emit(ctx, hooks.Event{
		Name:      "scenario.run." + string(run.Status),
		Extension: "scenario",
		Entity:    "ScenarioRun",
		EntityID:  run.ID.String(),
		Op:        op,
		At:        at,
		Payload:   run,
	})
}
