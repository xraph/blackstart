package cascade_test

// Coverage beyond the wire crossing itself: request validation, the domain
// event, and the collaborator checks. Split from engine_test.go so the tests
// proving the crossing is correct can land in one commit and this breadth
// can follow in the next, both self-tested, neither an all-code-then-all-
// tests split of the same functionality.

import (
	"context"
	"errors"
	"testing"

	"github.com/xraph/blackstart/extensions/cascade"
	cid "github.com/xraph/blackstart/extensions/cascade/id"
	"github.com/xraph/blackstart/extensions/cascade/store/memory"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/hooks"
	solverv1 "github.com/xraph/blackstart/proto/gen/go/blackstart/solver/v1"
)

func TestAnEngineNeedsItsCollaborators(t *testing.T) {
	simulator := &fakeSimulator{}

	if _, err := cascade.NewEngine(nil, simulator, nil); err == nil {
		t.Error("NewEngine accepted a nil store")
	}

	if _, err := cascade.NewEngine(memory.New(), nil, nil); err == nil {
		t.Error("NewEngine accepted a nil simulator")
	}
}

func TestAnInvalidRequestNeverReachesTheSimulator(t *testing.T) {
	cases := map[string]cascade.Request{
		"no model":    {},
		"no branches": {Model: &solverv1.NetworkModel{SBaseVa: 100e6}},
	}

	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			simulator := &fakeSimulator{response: settledResponse()}

			if _, err := engine(t, simulator, nil).Simulate(t.Context(), request); err == nil {
				t.Fatalf("Simulate accepted a request with %s", name)
			}

			if simulator.calls != 0 {
				t.Errorf("an invalid request reached the simulator %d times", simulator.calls)
			}
		})
	}
}

func TestTheRequestReachesTheSimulatorIntact(t *testing.T) {
	simulator := &fakeSimulator{response: settledResponse()}

	if _, err := engine(t, simulator, nil).Simulate(t.Context(), cascade.Request{
		Model:      model(),
		Initiating: []uint32{1},
		HorizonS:   30,
		MaxSteps:   500,
	}); err != nil {
		t.Fatalf("Simulate: %v", err)
	}

	if got := simulator.sawRequest.GetInitiating(); len(got) != 1 || got[0] != 1 {
		t.Errorf("the initiating outages did not survive: %v", got)
	}

	if got := simulator.sawRequest.GetHorizonS(); got != 30 {
		t.Errorf("horizon arrived as %v", got)
	}

	if got := simulator.sawRequest.GetMaxSteps(); got != 500 {
		t.Errorf("max steps arrived as %v", got)
	}
}

func TestAMissingRunIsNotFound(t *testing.T) {
	e := engine(t, &fakeSimulator{}, nil)

	if _, err := e.Get(t.Context(), cid.NewRun()); !errors.Is(err, bserr.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestASimulationEmitsADomainEvent(t *testing.T) {
	bus := hooks.NewBus()

	var seen []hooks.Event
	bus.Subscribe("cascade.simulated", func(_ context.Context, e hooks.Event) error {
		seen = append(seen, e)

		return nil
	})

	simulator := &fakeSimulator{response: settledResponse()}

	result, err := engine(t, simulator, bus).Simulate(t.Context(), cascade.Request{Model: model()})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}

	if len(seen) != 1 {
		t.Fatalf("got %d events, want one", len(seen))
	}

	if seen[0].EntityID != result.ID.String() {
		t.Errorf("the event names %q, want %q", seen[0].EntityID, result.ID)
	}
}

// A nil bus is the normal case for a CLI invocation.
func TestANilBusIsFine(t *testing.T) {
	simulator := &fakeSimulator{response: settledResponse()}

	if _, err := engine(t, simulator, nil).Simulate(
		t.Context(), cascade.Request{Model: model()},
	); err != nil {
		t.Fatalf("Simulate with no bus: %v", err)
	}
}
