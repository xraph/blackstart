package sweep_test

import (
	"errors"
	"strings"
	"testing"

	cid "github.com/xraph/blackstart/extensions/contingency/id"
	"github.com/xraph/blackstart/extensions/contingency/sweep"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

func screened(branch uint32, worst float64, violations ...sweep.Violation) sweep.Candidate {
	return sweep.Candidate{
		ContingencyID: "line",
		Branch:        branch,
		Screenability: sweep.Screened,
		Violations:    violations,
		WorstLoading:  new(worst),
	}
}

func unevaluated(branch uint32, why sweep.Screenability) sweep.Candidate {
	return sweep.Candidate{
		ContingencyID: "line",
		Branch:        branch,
		Screenability: why,
		// No loading. That absence is the point.
	}
}

func valid() sweep.Run {
	return sweep.Run{
		ID:            cid.NewRun(),
		ModelHash:     "abc123",
		SolverVersion: "0.1.0",
	}
}

func TestAWellFormedRunValidates(t *testing.T) {
	if err := valid().Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestARunThatCannotBeReproducedIsRejected(t *testing.T) {
	cases := map[string]func(*sweep.Run){
		"no identifier": func(r *sweep.Run) { r.ID = id.Nil },
		"no model":      func(r *sweep.Run) { r.ModelHash = "" },
		"no solver":     func(r *sweep.Run) { r.SolverVersion = "" },
	}

	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			r := valid()
			breakIt(&r)

			err := r.Validate()
			if err == nil {
				t.Fatalf("Validate accepted a run with %s", name)
			}

			if !errors.Is(err, bserr.ErrInvalid) {
				t.Errorf("got %v, want it to wrap ErrInvalid", err)
			}
		})
	}
}

func TestTheRejectionSaysWhyTheModelMatters(t *testing.T) {
	r := valid()
	r.ModelHash = ""

	err := r.Validate()
	if err == nil {
		t.Fatal("Validate accepted a run naming no model")
	}

	if !strings.Contains(err.Error(), "reproduced") {
		t.Errorf("message %q does not say why", err)
	}
}

// The ordering this package exists for. A radial outage that islands part of
// the network is usually the most consequential contingency on the list, and
// it is exactly the one the linear screen cannot evaluate. Sorting it by
// loading would put it last, behind every mildly loaded case.
func TestUnevaluatedContingenciesRankAheadOfEverything(t *testing.T) {
	run := valid()
	run.Candidates = []sweep.Candidate{
		screened(0, 1.8),
		screened(1, 0.4),
		unevaluated(2, sweep.Radial),
		screened(3, 1.2),
		unevaluated(4, sweep.NotApplicable),
	}

	ranked := run.Ranked()

	if ranked[0].Screenability.Evaluated() || ranked[1].Screenability.Evaluated() {
		t.Fatalf("ranking put an evaluated contingency first: %v",
			[]sweep.Screenability{ranked[0].Screenability, ranked[1].Screenability})
	}

	// Then the evaluated ones descend by loading.
	rest := ranked[2:]
	for i := range len(rest) - 1 {
		if *rest[i].WorstLoading < *rest[i+1].WorstLoading {
			t.Errorf("branch %d at %v came before branch %d at %v",
				rest[i].Branch, *rest[i].WorstLoading,
				rest[i+1].Branch, *rest[i+1].WorstLoading)
		}
	}
}

// Ranking must not mutate what was recorded. The run preserves the order the
// contingencies were evaluated in, because that is what was asked for; ranking
// is a view over it.
func TestRankingDoesNotDisturbTheRecord(t *testing.T) {
	run := valid()
	run.Candidates = []sweep.Candidate{
		screened(0, 0.4),
		screened(1, 1.8),
		screened(2, 1.2),
	}

	_ = run.Ranked()

	for index, candidate := range run.Candidates {
		if candidate.Branch != uint32(index) {
			t.Fatalf("ranking reordered the record: %v", run.Candidates)
		}
	}
}

// A ranking that changes between calls would make any golden over it noise.
func TestRankingIsReproducible(t *testing.T) {
	run := valid()
	run.Candidates = []sweep.Candidate{
		screened(3, 1.5),
		screened(1, 1.5),
		screened(2, 1.5),
		unevaluated(0, sweep.Radial),
	}

	first := run.Ranked()

	for range 20 {
		again := run.Ranked()
		for i := range first {
			if first[i].Branch != again[i].Branch {
				t.Fatalf("ranked %v then %v", branches(first), branches(again))
			}
		}
	}

	// Equal loadings break on branch index, ascending.
	if got := branches(first); got[1] != 1 || got[2] != 2 || got[3] != 3 {
		t.Errorf("ties ordered %v, want the radial first then ascending index", got)
	}
}

// An unevaluated contingency is concerning. It is the strongest signal the
// screen produces, and a helper that answered "no" would invert its meaning —
// which is the specific failure this whole package is shaped to prevent.
func TestAnUnevaluatedContingencyIsConcerning(t *testing.T) {
	for _, why := range []sweep.Screenability{sweep.Radial, sweep.NotApplicable} {
		if !unevaluated(0, why).Concerning() {
			t.Errorf("a %q contingency was reported as unconcerning", why)
		}
	}

	// A screened contingency with no violations is not.
	if screened(0, 0.4).Concerning() {
		t.Error("a screened contingency within its ratings was reported as concerning")
	}

	// One with violations is.
	withViolation := screened(0, 1.4, sweep.Violation{Branch: 2, Loading: 1.4})
	if !withViolation.Concerning() {
		t.Error("a screened contingency with a violation was reported as unconcerning")
	}
}

func TestConcerningReturnsTheWorstFirst(t *testing.T) {
	run := valid()
	run.Candidates = []sweep.Candidate{
		screened(0, 0.4),
		screened(1, 1.8, sweep.Violation{Branch: 3, Loading: 1.8}),
		unevaluated(2, sweep.Radial),
		screened(3, 1.2, sweep.Violation{Branch: 0, Loading: 1.2}),
	}

	got := run.Concerning()

	if len(got) != 3 {
		t.Fatalf("got %d concerning candidates, want three", len(got))
	}

	if got[0].Screenability != sweep.Radial {
		t.Errorf("the radial outage is not first: %v", branches(got))
	}

	if got[1].Branch != 1 || got[2].Branch != 3 {
		t.Errorf("the violated contingencies ordered %v, want worst first", branches(got))
	}
}

// Only Screened means a number was produced. The other two are the absence of
// one, and conflating them with a zero is how a screen misleads.
func TestOnlyScreenedCountsAsEvaluated(t *testing.T) {
	if !sweep.Screened.Evaluated() {
		t.Error("Screened should be evaluated")
	}

	for _, why := range []sweep.Screenability{sweep.Radial, sweep.NotApplicable, ""} {
		if why.Evaluated() {
			t.Errorf("%q should not count as evaluated", why)
		}
	}
}

// Absent loading, not zero loading. The distinction is the whole reason
// WorstLoading is a pointer.
func TestAnUnevaluatedContingencyCarriesNoLoading(t *testing.T) {
	if unevaluated(0, sweep.Radial).WorstLoading != nil {
		t.Error("a contingency nobody could screen came back with a loading")
	}
}

func branches(candidates []sweep.Candidate) []uint32 {
	out := make([]uint32, len(candidates))
	for i, candidate := range candidates {
		out[i] = candidate.Branch
	}

	return out
}
