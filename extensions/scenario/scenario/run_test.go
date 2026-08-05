package scenario_test

import (
	"testing"

	cid "github.com/xraph/blackstart/extensions/cascade/id"
	crid "github.com/xraph/blackstart/extensions/contingency/id"
	"github.com/xraph/blackstart/extensions/scenario/scenario"
)

// A failed run must say WHERE it failed. The pipeline has five steps that
// fail for different reasons, and a run that failed without naming one is a
// run nobody can act on.
func TestAFailedRunNamesItsStep(t *testing.T) {
	t.Parallel()

	r := scenario.ScenarioRun{Status: scenario.StatusFailed}

	if err := r.Validate(); err == nil {
		t.Fatal("want an error: a failed run with no failure recorded")
	}

	r.Failure = &scenario.Failure{Step: scenario.StepRatings, Reason: "no ampacity for the cycle"}

	if err := r.Validate(); err != nil {
		t.Fatalf("want nil once the failure names a step, got %v", err)
	}
}

// A complete run must name the children it produced, or nothing links the
// scenario to its own answer.
func TestACompleteRunNamesItsChildren(t *testing.T) {
	t.Parallel()

	r := scenario.ScenarioRun{Status: scenario.StatusComplete}

	if err := r.Validate(); err == nil {
		t.Fatal("want an error: a complete run with no sweep or cascade")
	}
}

// A queued run has neither, and that is legitimate.
func TestAQueuedRunNeedsNeither(t *testing.T) {
	t.Parallel()

	r := scenario.ScenarioRun{Status: scenario.StatusQueued}

	if err := r.Validate(); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

// A queued run has not reached the rating step, so both fields must be nil
// — not an int zero value, which would read as "rated zero of zero" rather
// than "has not looked yet".
func TestAQueuedRunHasNoWeatherRatingYet(t *testing.T) {
	t.Parallel()

	r := scenario.ScenarioRun{Status: scenario.StatusQueued}

	if r.WeatherRated != nil {
		t.Errorf("WeatherRated = %v, want nil before the rating step runs", r.WeatherRated)
	}

	if r.BranchCount != nil {
		t.Errorf("BranchCount = %v, want nil before the rating step runs", r.BranchCount)
	}
}

// Once the rating step completes, WeatherRated and BranchCount are set
// together — and the rated count can legitimately be zero. That has to be a
// pointer to 0, not nil: nil means "we have not looked", a pointer to 0
// means "we looked, and rated nothing". Collapsing those two into one bare
// int is exactly the bug this pair of fields exists to prevent.
func TestWeatherRatingIsSetTogetherEvenWhenRatedIsZero(t *testing.T) {
	t.Parallel()

	rated, branches := 0, 2000

	r := scenario.ScenarioRun{
		Status:       scenario.StatusRunning,
		WeatherRated: &rated,
		BranchCount:  &branches,
	}

	if err := r.Validate(); err != nil {
		t.Fatalf("want nil, got %v", err)
	}

	if r.WeatherRated == nil {
		t.Fatal("WeatherRated is nil; the zero-rated case must be a pointer to 0, not absent")
	}

	if *r.WeatherRated != 0 {
		t.Errorf("WeatherRated = %d, want 0", *r.WeatherRated)
	}

	if r.BranchCount == nil || *r.BranchCount != 2000 {
		t.Errorf("BranchCount = %v, want a pointer to 2000", r.BranchCount)
	}
}

// WeatherRated and BranchCount are one observation, not two independent
// ones: a run with one set and the other nil is not a state Validate lets
// through.
func TestWeatherRatingCannotBeSetPartially(t *testing.T) {
	t.Parallel()

	rated := 3

	r := scenario.ScenarioRun{Status: scenario.StatusRunning, WeatherRated: &rated}

	if err := r.Validate(); err == nil {
		t.Fatal("want an error: WeatherRated is set but BranchCount is nil")
	}
}

// A fallback count with no rating observation to count against is not a
// state Validate lets through.
//
// The number is a count of the overrides the rating step emitted, so
// WeatherRated is its denominator: "17 fell back" says nothing without "of
// how many". A run carrying the count while claiming never to have rated
// anything is not a partial record, it is a contradiction.
func TestAVoltageBaseFallbackNeedsTheRatingItCounts(t *testing.T) {
	t.Parallel()

	fellBack := 17

	r := scenario.ScenarioRun{Status: scenario.StatusRunning, BaseVoltageFallback: &fellBack}

	if err := r.Validate(); err == nil {
		t.Fatal("want an error: a fallback count with no WeatherRated to count against")
	}
}

// The implication runs one way only, and the asymmetry is the decision.
//
// A run that rated its branches without recording how many fell back is
// exactly what every run stored before this column existed looks like: the
// rating pair set, the fallback unknown. That is a legitimate historical
// record, not an incoherent one, and Validate tying the three together
// would make it a run nothing could re-store. Tightening this to a
// biconditional is the production change this test exists to catch.
func TestTheRatingPairDoesNotRequireAFallbackCount(t *testing.T) {
	t.Parallel()

	rated, branches := 1841, 2000

	r := scenario.ScenarioRun{
		Status:       scenario.StatusRunning,
		WeatherRated: &rated,
		BranchCount:  &branches,
	}

	if err := r.Validate(); err != nil {
		t.Fatalf("want nil: a run may record its rating without knowing the fallback, got %v", err)
	}
}

// A fallback of zero is a measurement, and has to survive as one.
//
// Nil says the count was never taken; a pointer to 0 says every rated branch
// converted against its own from-bus's stated voltage. The second is the
// answer a well-formed model gives, and reporting it as "unknown" would make
// the model that states every voltage indistinguishable from the one nobody
// has looked at.
func TestAFallbackOfZeroIsAMeasurementNotAnAbsence(t *testing.T) {
	t.Parallel()

	rated, branches, fellBack := 1841, 2000, 0

	r := scenario.ScenarioRun{
		Status:              scenario.StatusRunning,
		WeatherRated:        &rated,
		BranchCount:         &branches,
		BaseVoltageFallback: &fellBack,
	}

	if err := r.Validate(); err != nil {
		t.Fatalf("want nil, got %v", err)
	}

	if r.BaseVoltageFallback == nil {
		t.Fatal("BaseVoltageFallback is nil; falling back nowhere must be a pointer to 0, not absent")
	}

	if *r.BaseVoltageFallback != 0 {
		t.Errorf("BaseVoltageFallback = %d, want 0", *r.BaseVoltageFallback)
	}
}

// A run finishes complete or failed and nothing else. Writing a queued status
// over a run that already produced an answer would put it back in the queue
// for a second worker to execute again.
func TestOnlyATerminalStatusIsFinishable(t *testing.T) {
	t.Parallel()

	base := scenario.ScenarioRun{
		Status:       scenario.StatusComplete,
		SweepRunID:   crid.NewRun(),
		CascadeRunID: cid.NewRun(),
	}

	if err := base.Finishable(); err != nil {
		t.Fatalf("a complete run with both children is finishable, got %v", err)
	}

	failed := scenario.ScenarioRun{
		Status:  scenario.StatusFailed,
		Failure: &scenario.Failure{Step: scenario.StepScreen, Reason: "the solver refused"},
	}

	if err := failed.Finishable(); err != nil {
		t.Fatalf("a failed run naming its step is finishable, got %v", err)
	}

	for _, status := range []scenario.Status{scenario.StatusQueued, scenario.StatusRunning, ""} {
		notTerminal := base
		notTerminal.Status = status

		if err := notTerminal.Finishable(); err == nil {
			t.Errorf("status %q is finishable; only complete and failed are", status)
		}
	}
}

// Finishable is Validate plus the terminal-state rule, not a replacement for
// it: a failed run that does not name its step is still a run nobody can act
// on, whichever method is asked.
func TestFinishableStillRejectsAFailedRunWithNoStep(t *testing.T) {
	t.Parallel()

	r := scenario.ScenarioRun{Status: scenario.StatusFailed}

	if err := r.Finishable(); err == nil {
		t.Fatal("want an error: a failed run with no failure recorded")
	}
}
