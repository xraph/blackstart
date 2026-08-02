package powerflow_test

import (
	"context"
	"testing"

	"github.com/xraph/blackstart/extensions/powerflow"
	"github.com/xraph/blackstart/extensions/powerflow/solution"
	"github.com/xraph/blackstart/pkg/hooks"
)

// The event carries enough for a subscriber to act without a second query.
func TestASolveEmitsADomainEvent(t *testing.T) {
	bus := hooks.NewBus()

	var seen []hooks.Event
	bus.Subscribe("powerflow.solved", func(_ context.Context, e hooks.Event) error {
		seen = append(seen, e)

		return nil
	})

	e := engine(t, &fakeSolver{response: response()}, bus)

	got, err := e.Solve(t.Context(), powerflow.Request{
		Model:  model(),
		Method: solution.MethodAC,
	})
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}

	if len(seen) != 1 {
		t.Fatalf("got %d events, want one", len(seen))
	}

	if seen[0].EntityID != got.ID.String() {
		t.Errorf("the event names %q, want %q", seen[0].EntityID, got.ID)
	}

	if seen[0].Extension != "powerflow" {
		t.Errorf("the event came from %q", seen[0].Extension)
	}
}

// A nil bus is the normal case for a CLI invocation and must not be a panic.
func TestANilBusIsFine(t *testing.T) {
	e := engine(t, &fakeSolver{response: response()}, nil)

	if _, err := e.Solve(t.Context(), powerflow.Request{
		Model:  model(),
		Method: solution.MethodAC,
	}); err != nil {
		t.Fatalf("Solve with no bus: %v", err)
	}
}
