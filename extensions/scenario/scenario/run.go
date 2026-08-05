package scenario

import (
	"fmt"
	"time"

	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Status is where a run is in its lifecycle.
//
// These four spellings are not this extension's to invent: they are exactly
// what web/apps/viewer/src/search/params.ts's runsSearch already declares
// for the console's run-list filter. A different spelling here would not be
// a new convention, it would be a console that silently stops matching runs.
type Status string

const (
	// StatusQueued means the run has been recorded and is waiting for a
	// worker to claim it. Neither a Failure nor either child id is expected
	// yet — that is the legitimate, ordinary state of a run nobody has
	// touched.
	StatusQueued Status = "queued"

	// StatusRunning means a worker has claimed the run and is executing the
	// pipeline against it.
	StatusRunning Status = "running"

	// StatusComplete means the pipeline finished and produced an answer.
	StatusComplete Status = "complete"

	// StatusFailed means the pipeline stopped before producing an answer.
	StatusFailed Status = "failed"
)

// Step names one stage of the scenario pipeline.
type Step string

const (
	// StepResolve resolves the manifest's named model, forecast and hazard
	// configuration into the concrete inputs the rest of the pipeline runs
	// against.
	StepResolve Step = "resolve"

	// StepRatings rates the branches the weather cycle covers.
	StepRatings Step = "ratings"

	// StepScreen screens contingencies against the rated network.
	StepScreen Step = "screen"

	// StepCascade simulates the cascade for the branches the screen flagged.
	StepCascade Step = "cascade"
)

// Failure names where a run stopped and why.
type Failure struct {
	// Step is the pipeline stage that failed.
	Step Step `json:"step"`

	// Reason is a human-readable explanation, not a machine-parsed code —
	// the four Step values already carry the classification a dashboard
	// would filter on.
	Reason string `json:"reason"`
}

// Run is one execution of a scenario: the frozen manifest it ran against,
// where it is in its lifecycle, and — once that lifecycle reaches a terminal
// state — how it got there.
type Run struct {
	// ID identifies this run.
	ID id.ID `json:"id"`

	// ScenarioID names the scenario this run executes.
	ScenarioID id.ID `json:"scenario_id"`

	// Manifest is the frozen copy of the scenario's manifest at the moment
	// this run was queued. Frozen rather than a reference, because a
	// scenario's manifest could in principle be edited later, and a run must
	// stay reproducible from its own record even if the scenario it came
	// from changed underneath it.
	Manifest Manifest `json:"manifest"`

	// Status is where this run is in its lifecycle.
	Status Status `json:"status"`

	// Failure names where the run stopped, when Status is StatusFailed.
	// Absent otherwise — a run that has not failed has no failure to report,
	// which is different from a failure with no step.
	Failure *Failure `json:"failure,omitempty"`

	// SweepRunID and CascadeRunID name the child runs this scenario produced,
	// once Status is StatusComplete. Absent otherwise. Together they are what
	// links a scenario to its own answer — without both, nothing connects
	// the manifest that was requested to the numbers that came out of it.
	SweepRunID   id.ID `json:"sweep_run_id,omitzero"`
	CascadeRunID id.ID `json:"cascade_run_id,omitzero"`

	// WeatherRated is how many branches the weather-rating step actually
	// rated. BranchCount is how many branches the model has in total.
	//
	// Both exist because a scenario that weather-rated three branches out of
	// two thousand would otherwise look identical, in a summary that only
	// reports "rated", to one that rated all two thousand — and the second
	// is what a reader assumes unless the record says otherwise.
	//
	// Both are pointers, and nil until the rating step runs, for the same
	// reason run.CascadeRun.Island's Frequency/Nadir/Rocof are pointers: a
	// plain int cannot tell "rated zero of two thousand" — a real finding,
	// and the whole reason this pair exists — apart from "has not looked
	// yet". A bare 0 would silently collapse those into the same value, on
	// the struct and on the wire, exactly where the distinction matters
	// most: a run that failed at StepScreen after rating nothing would read
	// identically to one that never reached the rating step at all.
	//
	// The two are set together, at the point the rating step completes,
	// because they are one observation about one run: Validate rejects a
	// run where one is nil and the other is not.
	WeatherRated *int `json:"weather_rated,omitempty"`
	BranchCount  *int `json:"branch_count,omitempty"`

	// SolverVersion is the solver that produced the cascade result, read
	// from the solver's own response rather than configured into the engine
	// — the same provenance rule run.CascadeRun.SolverVersion follows.
	SolverVersion string `json:"solver_version"`

	// QueuedAt, StartedAt and FinishedAt bound the run's lifecycle: when it
	// was recorded, when a worker claimed it, and when it reached a terminal
	// state. StartedAt and FinishedAt are zero until Claim and completion
	// happen, respectively.
	QueuedAt   time.Time `json:"queued_at"`
	StartedAt  time.Time `json:"started_at,omitzero"`
	FinishedAt time.Time `json:"finished_at,omitzero"`
}

// Validate reports whether a run is coherent enough to store.
//
// It is strict about the two terminal states specifically. A failed run
// without a Failure naming its step is a run nobody can act on — the
// pipeline has four steps that fail for different reasons, and "it failed"
// alone does not say which one to look at. A complete run without both child
// ids is a run with no answer attached to it: nothing links the scenario
// back to the sweep and cascade that were supposedly its result.
//
// It is also strict about WeatherRated and BranchCount regardless of
// status: they are one observation about one run, recorded together at the
// point the rating step completes, so a run with one set and the other nil
// is not a state the type should tolerate constructing.
func (r Run) Validate() error {
	if r.Status == StatusFailed {
		if r.Failure == nil || r.Failure.Step == "" {
			return fmt.Errorf(
				"%w: a failed run must name the step it failed at, or nobody "+
					"can act on it", bserr.ErrInvalid)
		}
	}

	if r.Status == StatusComplete {
		if r.SweepRunID.IsZero() || r.CascadeRunID.IsZero() {
			return fmt.Errorf(
				"%w: a complete run must name both child runs, or nothing "+
					"links the scenario to its own answer", bserr.ErrInvalid)
		}
	}

	if (r.WeatherRated == nil) != (r.BranchCount == nil) {
		return fmt.Errorf(
			"%w: weather rated and branch count are one observation and must "+
				"be set together", bserr.ErrInvalid)
	}

	return nil
}
