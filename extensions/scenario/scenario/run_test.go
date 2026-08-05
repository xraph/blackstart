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
