package scenario

import (
	"context"
	"fmt"

	"github.com/xraph/blackstart/extensions/cascade"
	scn "github.com/xraph/blackstart/extensions/scenario/scenario"
)

// Execute runs a claimed run's pipeline and records what it produced.
//
// Five steps: resolve the manifest and verify what it pinned, rate the
// branches from the weather, screen contingencies against the rated network,
// cascade the worst of them, and record. Each stage's error is wrapped into a
// Failure naming its Step before the run is stored, because the pipeline has
// five places to fail and a run that failed without saying where is one
// nobody can act on.
//
// The scenario is never read here. Everything this executes against came off
// the run's own frozen manifest, which is what makes the stored result
// citable.
//
// The run itself IS re-read, and that is not a contradiction — the manifest
// is frozen against the scenario, not against the store. The argument is a
// copy a caller has been holding, and a copy can be stale: a run finished
// elsewhere, or requeued after a timeout, still reads "running" in a variable
// captured before that happened. Trusting it would screen and cascade in
// full, write a crun_… and a casc_… through two other extensions, and only
// then be refused by Finish — leaving both child records orphaned with no
// scenario run pointing at them.
//
// # Errors
//
// A pipeline failure is not an error: it is the run, stored failed, with its
// step and reason on it, and returned. Execute returns an error only when the
// outcome could not be recorded at all — which is the one case a worker
// cannot turn into a stored fact and has to retry or report. A caller reads
// Status to learn what happened; err answers a different question.
func (e *Engine) Execute(ctx context.Context, in scn.Run) (scn.Run, error) {
	in, err := e.store.Runs().Get(ctx, in.ID)
	if err != nil {
		return scn.Run{}, fmt.Errorf("scenario: reading the run to execute: %w", err)
	}

	if in.Status != scn.StatusRunning {
		return scn.Run{}, fmt.Errorf(
			"scenario: run %s is %q in the store, not running; a run is executed "+
				"after it is claimed, and the claim is what stops two workers from "+
				"executing one scenario twice", in.ID, in.Status)
	}

	resolved, err := e.resolve(ctx, in.Manifest)
	if err != nil {
		return e.fail(ctx, in, scn.StepResolve, err)
	}

	rated, err := rate(
		ctx, resolved.model, e.collaborator.Geo, e.collaborator.Hazard,
		resolved.issuedAt, in.Manifest.ValidAt)
	if err != nil {
		return e.fail(ctx, in, scn.StepRatings, err)
	}

	// Recorded before the coverage is judged, and before anything downstream
	// runs. A run that failed after rating nothing and a run that never
	// reached the rating step are different findings, and these two fields
	// are the only thing that tells them apart.
	in.WeatherRated, in.BranchCount = &rated.WeatherRated, &rated.BranchCount

	if err := ratingGap(in.Manifest, rated); err != nil {
		return e.fail(ctx, in, scn.StepRatings, err)
	}

	screened, worst, err := e.screen(ctx, in.Manifest, resolved.model, rated.Overrides)
	if err != nil {
		return e.fail(ctx, in, scn.StepScreen, err)
	}

	in.SweepRunID = screened.ID

	// The same overrides the screen ran against, so the cascade simulates the
	// network the ranking came from rather than the nameplate one.
	simulated, err := e.collaborator.Simulator.Simulate(ctx, cascade.Request{
		Model:     resolved.model,
		Overrides: rated.Overrides,
		// One outage, the worst the screen ranked. Everything after it is the
		// simulator's consequence rather than a further instruction.
		Initiating: []uint32{worst.Branch},
	})
	if err != nil {
		return e.fail(ctx, in, scn.StepCascade, err)
	}

	in.CascadeRunID = simulated.ID
	// Read from the cascade's own record rather than configured into this
	// engine: a run states what produced it, and a second source of truth for
	// the solver version is one that can be stale.
	in.SolverVersion = simulated.SolverVersion
	in.Status = scn.StatusComplete
	in.FinishedAt = e.now().UTC()

	stored, err := e.store.Runs().Finish(ctx, in)
	if err != nil {
		return scn.Run{}, fmt.Errorf("scenario: recording run %s complete: %w", in.ID, err)
	}

	e.emit(ctx, stored)

	return stored, nil
}

// fail records where a run stopped and why, and returns the stored run.
//
// The cause becomes the Failure's reason and the step becomes its step, so a
// reader of the record knows which of the five stages to look at. Storing it
// is what makes a failed run a fact rather than a log line.
func (e *Engine) fail(
	ctx context.Context, in scn.Run, step scn.Step, cause error,
) (scn.Run, error) {
	in.Status = scn.StatusFailed
	in.Failure = &scn.Failure{Step: step, Reason: cause.Error()}
	in.FinishedAt = e.now().UTC()

	stored, err := e.store.Runs().Finish(ctx, in)
	if err != nil {
		// Both wrapped, so errors.Is finds either: the pipeline failure is
		// what happened and the store failure is why nobody will find it in
		// the record.
		return scn.Run{}, fmt.Errorf(
			"scenario: run %s failed at %s (%w), and recording that failed too: %w",
			in.ID, step, cause, err)
	}

	e.emit(ctx, stored)

	return stored, nil
}
