package powerflow_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/powerflow"
	pfid "github.com/xraph/blackstart/extensions/powerflow/id"
	"github.com/xraph/blackstart/extensions/powerflow/solution"
	"github.com/xraph/blackstart/pkg/bserr"
	solverv1 "github.com/xraph/blackstart/proto/gen/go/blackstart/solver/v1"
)

func TestTheRequestedMethodReachesTheSolver(t *testing.T) {
	for _, tc := range []struct {
		domain solution.Method
		wire   solverv1.Method
	}{
		{solution.MethodDC, solverv1.Method_METHOD_DC},
		{solution.MethodAC, solverv1.Method_METHOD_AC},
	} {
		solver := &fakeSolver{response: response()}
		e := engine(t, solver, nil)

		if _, err := e.Solve(t.Context(), powerflow.Request{
			Model:  model(),
			Method: tc.domain,
		}); err != nil {
			t.Fatalf("Solve: %v", err)
		}

		if solver.sawRequest.GetMethod() != tc.wire {
			t.Errorf("%q was sent as %v, want %v",
				tc.domain, solver.sawRequest.GetMethod(), tc.wire)
		}
	}
}

func TestAnInvalidRequestNeverReachesTheSolver(t *testing.T) {
	cases := map[string]powerflow.Request{
		"no model":       {Method: solution.MethodAC},
		"no buses":       {Model: &solverv1.NetworkModel{SBaseVa: 100e6}, Method: solution.MethodAC},
		"no method":      {Model: model()},
		"unknown method": {Model: model(), Method: "gauss-seidel"},
	}

	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			solver := &fakeSolver{response: response()}
			e := engine(t, solver, nil)

			if _, err := e.Solve(t.Context(), request); err == nil {
				t.Fatalf("Solve accepted a request with %s", name)
			}

			if solver.calls != 0 {
				t.Errorf("an invalid request reached the solver %d times", solver.calls)
			}
		})
	}
}

// A solver failure must not leave a partial record behind. Half a solution is
// worse than none: it would sit in the store looking like an answer.
func TestASolverFailureStoresNothing(t *testing.T) {
	solver := &fakeSolver{err: errors.New("the solver fell over")}
	e := engine(t, solver, nil)

	_, err := e.Solve(t.Context(), powerflow.Request{
		Model:  model(),
		Method: solution.MethodAC,
	})
	if err == nil {
		t.Fatal("Solve succeeded against a failing solver")
	}

	if !strings.Contains(err.Error(), "fell over") {
		t.Errorf("the solver's own error was lost: %v", err)
	}

	found, err := e.ListByModel(t.Context(), "abc123", 0)
	if err != nil {
		t.Fatalf("ListByModel: %v", err)
	}

	if len(found) != 0 {
		t.Errorf("a failed solve left %d solutions behind", len(found))
	}
}

func TestListByModelReturnsMostRecentFirst(t *testing.T) {
	solver := &fakeSolver{response: response()}
	e := engine(t, solver, nil)

	var ids []string

	for range 3 {
		got, err := e.Solve(t.Context(), powerflow.Request{
			Model:  model(),
			Method: solution.MethodAC,
		})
		if err != nil {
			t.Fatalf("Solve: %v", err)
		}

		ids = append(ids, got.ID.String())
	}

	found, err := e.ListByModel(t.Context(), "abc123", 0)
	if err != nil {
		t.Fatalf("ListByModel: %v", err)
	}

	if len(found) != 3 {
		t.Fatalf("got %d solutions, want 3", len(found))
	}

	if found[0].ID.String() != ids[2] {
		t.Errorf("first result is %s, want the most recent %s", found[0].ID, ids[2])
	}

	// And a limit truncates from the recent end, which is what a caller
	// asking for "the latest few" means.
	limited, err := e.ListByModel(t.Context(), "abc123", 2)
	if err != nil {
		t.Fatalf("ListByModel: %v", err)
	}

	if len(limited) != 2 || limited[0].ID != found[0].ID {
		t.Errorf("a limit of 2 gave %d results starting at %s", len(limited), limited[0].ID)
	}
}

func TestAMissingSolutionIsNotFound(t *testing.T) {
	e := engine(t, &fakeSolver{response: response()}, nil)

	_, err := e.Get(t.Context(), pfid.NewSolution())
	if !errors.Is(err, bserr.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}
