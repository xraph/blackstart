package scenario_test

import (
	"testing"

	"github.com/xraph/blackstart/extensions/scenario/scenario"
)

// A failed run must say WHERE it failed. The pipeline has five steps that
// fail for different reasons, and a run that failed without naming one is a
// run nobody can act on.
func TestAFailedRunNamesItsStep(t *testing.T) {
	t.Parallel()

	r := scenario.Run{Status: scenario.StatusFailed}

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

	r := scenario.Run{Status: scenario.StatusComplete}

	if err := r.Validate(); err == nil {
		t.Fatal("want an error: a complete run with no sweep or cascade")
	}
}

// A queued run has neither, and that is legitimate.
func TestAQueuedRunNeedsNeither(t *testing.T) {
	t.Parallel()

	r := scenario.Run{Status: scenario.StatusQueued}

	if err := r.Validate(); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

// A queued run has not reached the rating step, so both fields must be nil
// — not an int zero value, which would read as "rated zero of zero" rather
// than "has not looked yet".
func TestAQueuedRunHasNoWeatherRatingYet(t *testing.T) {
	t.Parallel()

	r := scenario.Run{Status: scenario.StatusQueued}

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

	r := scenario.Run{
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

	r := scenario.Run{Status: scenario.StatusRunning, WeatherRated: &rated}

	if err := r.Validate(); err == nil {
		t.Fatal("want an error: WeatherRated is set but BranchCount is nil")
	}
}
