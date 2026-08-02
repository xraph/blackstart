package solution_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/powerflow/id"
	"github.com/xraph/blackstart/extensions/powerflow/solution"
	"github.com/xraph/blackstart/pkg/bserr"
	bsid "github.com/xraph/blackstart/pkg/id"
)

func valid() solution.Solution {
	return solution.Solution{
		ID:            id.NewSolution(),
		ModelHash:     "abc123",
		Method:        solution.MethodAC,
		SolverVersion: "0.1.0",
		Buses:         []solution.Bus{{VMagnitude: 1.0}},
	}
}

func TestAWellFormedSolutionValidates(t *testing.T) {
	if err := valid().Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// Every one of these makes a solution unreproducible, which is the property
// spec §2.5 requires of every number this platform reports. Storing one anyway
// would put a figure in the record that nothing can trace back.
func TestASolutionThatCannotBeReproducedIsRejected(t *testing.T) {
	cases := map[string]func(*solution.Solution){
		"no identifier":  func(s *solution.Solution) { s.ID = bsid.Nil },
		"no model":       func(s *solution.Solution) { s.ModelHash = "" },
		"no method":      func(s *solution.Solution) { s.Method = "" },
		"unknown method": func(s *solution.Solution) { s.Method = "gauss-seidel" },
		"no solver":      func(s *solution.Solution) { s.SolverVersion = "" },
		"nothing solved": func(s *solution.Solution) { s.Buses = nil },
	}

	for name, break_ := range cases {
		t.Run(name, func(t *testing.T) {
			s := valid()
			break_(&s)

			err := s.Validate()
			if err == nil {
				t.Fatalf("Validate accepted a solution with %s", name)
			}

			if !errors.Is(err, bserr.ErrInvalid) {
				t.Errorf("got %v, want it to wrap ErrInvalid", err)
			}
		})
	}
}

// The message has to say why, not just that. "invalid solution" sends the
// reader back to the source; naming the missing field does not.
func TestTheRejectionSaysWhatIsMissing(t *testing.T) {
	s := valid()
	s.ModelHash = ""

	err := s.Validate()
	if err == nil {
		t.Fatal("Validate accepted a solution naming no model")
	}

	if !strings.Contains(err.Error(), "reproduced") {
		t.Errorf("message %q does not say why a model hash is required", err)
	}
}

// An unrated branch has an unknown limit, not a zero one. Reporting it as
// overloaded would turn missing data into a finding, on a platform whose
// entire output is findings.
func TestAnUnratedBranchIsNeverOverloaded(t *testing.T) {
	unrated := solution.Branch{PFlow: 1e6, Loading: 0}

	for _, threshold := range []float64{0, 0.5, 1.0, 2.0} {
		if unrated.Overloaded(threshold) {
			t.Errorf("an unrated branch was reported overloaded at threshold %v", threshold)
		}
	}
}

// The threshold is the caller's: an emergency rating differs from a long-term
// one, and baking one in would assert a policy this package cannot assert.
func TestTheOverloadThresholdIsTheCallersChoice(t *testing.T) {
	branch := solution.Branch{Loading: 1.05}

	if !branch.Overloaded(1.0) {
		t.Error("a branch at 105% was not overloaded against a 100% threshold")
	}

	if branch.Overloaded(1.10) {
		t.Error("a branch at 105% was overloaded against a 110% threshold")
	}
}

// Worst first, so a caller that truncates keeps what matters.
func TestViolationsComeBackWorstFirst(t *testing.T) {
	s := valid()
	s.Branches = []solution.Branch{
		{Loading: 1.2},
		{Loading: 0.9},
		{Loading: 1.8},
		{Loading: 1.5},
		{Loading: 0.0},
	}

	got := s.Violations(1.0)

	want := []int{2, 3, 0}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// Ties break on index so a report reads the same way every run. Two branches
// at identical loading is not exotic — a symmetric network produces it — and
// an order that depends on the sort implementation would make a golden file
// noise.
func TestEqualLoadingsBreakOnIndex(t *testing.T) {
	s := valid()
	s.Branches = []solution.Branch{
		{Loading: 1.5},
		{Loading: 1.5},
		{Loading: 1.5},
	}

	first := s.Violations(1.0)

	for range 20 {
		again := s.Violations(1.0)
		for i := range first {
			if first[i] != again[i] {
				t.Fatalf("the same solution ordered %v then %v", first, again)
			}
		}
	}

	if first[0] != 0 || first[1] != 1 || first[2] != 2 {
		t.Errorf("ties ordered %v, want ascending index", first)
	}
}

func TestNoViolationsIsEmptyRatherThanNil(t *testing.T) {
	s := valid()
	s.Branches = []solution.Branch{{Loading: 0.5}, {Loading: 0.9}}

	if got := s.Violations(1.0); len(got) != 0 {
		t.Errorf("got %v, want nothing over the threshold", got)
	}
}

// A NaN loading must not be reported as a violation. It means the solve
// produced nothing usable for that branch, which is a different problem from
// an overload and calls for a different response.
func TestANaNLoadingIsNotAViolation(t *testing.T) {
	branch := solution.Branch{Loading: math.NaN()}

	if branch.Overloaded(1.0) {
		t.Error("a NaN loading was reported as an overload")
	}
}

func TestOnlyKnownMethodsAreValid(t *testing.T) {
	for _, m := range []solution.Method{solution.MethodDC, solution.MethodAC} {
		if !m.Valid() {
			t.Errorf("%q should be a valid method", m)
		}
	}

	for _, m := range []solution.Method{"", "DC", "newton", "gauss-seidel"} {
		if m.Valid() {
			t.Errorf("%q should not be a valid method", m)
		}
	}
}
