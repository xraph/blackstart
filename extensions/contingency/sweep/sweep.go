// Package sweep models an N-1 contingency analysis and its results.
//
// The distinction this package exists to preserve is between a screen and a
// verification. A DC screen is cheap and approximate; it produces candidates.
// An AC verification is expensive and exact for the model; it produces
// findings. They are different types here so that reporting one as the other
// takes deliberate effort, because a linearization presented as an answer is
// the specific way this kind of tool misleads people.
//
// The third state matters as much as either. A contingency the screen could
// not evaluate — a radial outage that islands part of the network — is not
// safe and not a finding. It is unknown, and unknown must not sort as safe.
package sweep

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Screenability is what the DC screen could say about one contingency.
type Screenability string

const (
	// Screened means the outage redistributes onto other branches and was
	// evaluated.
	Screened Screenability = "screened"

	// Radial means the outage disconnects the network, so its flow has
	// nowhere to go. A real contingency, but not one a linear factor can
	// answer — it has to be simulated.
	Radial Screenability = "radial"

	// NotApplicable means the branch was already out of service, so its
	// outage is not a contingency at all.
	NotApplicable Screenability = "not-applicable"
)

// Evaluated reports whether the screen produced a number for this outcome.
func (s Screenability) Evaluated() bool { return s == Screened }

// Violation is a branch loaded past its rating.
//
// The json tags are load-bearing rather than decorative: candidates are
// persisted as an encoded column, so a renamed field without a tag would
// silently change the stored format and make every existing run unreadable.
type Violation struct {
	// Branch is the index in the model's branch list.
	Branch uint32 `json:"branch"`

	// PFlow is the post-contingency flow, per unit.
	PFlow float64 `json:"p_flow"`

	// Rating is the limit it exceeded, per unit.
	Rating float64 `json:"rating"`

	// Loading is flow magnitude over rating, above the threshold by
	// construction.
	Loading float64 `json:"loading"`
}

// Candidate is one contingency as the screen saw it.
//
// A candidate, not a finding. The DC screen is exact for the DC model and
// approximate for the network, so what it produces is a ranking to verify
// rather than a result to report.
type Candidate struct {
	// ContingencyID names what was outaged, carried through from the request
	// so a report can print a line name rather than an index.
	ContingencyID string `json:"contingency_id"`

	// Branch is the index that was taken out of service.
	Branch uint32 `json:"branch"`

	// Screenability is what the screen could say.
	Screenability Screenability `json:"screenability"`

	// Violations are branches over their rating, worst first.
	Violations []Violation `json:"violations"`

	// WorstLoading is the highest loading seen anywhere, including branches
	// within their rating, so a contingency with no violations still ranks
	// against others.
	//
	// A pointer because absent and zero are different. A contingency the
	// screen could not evaluate has no loading, and representing that as zero
	// would rank the most consequential outage below every mildly loaded one.
	WorstLoading *float64 `json:"worst_loading,omitempty"`
}

// Concerning reports whether this candidate warrants verification.
//
// An unevaluated contingency counts. It is the strongest signal the screen
// can produce and a helper that answered "no" for it would be the exact
// inversion this package exists to prevent.
func (c Candidate) Concerning() bool {
	return !c.Screenability.Evaluated() || len(c.Violations) > 0
}

// Run is one execution of a contingency analysis.
type Run struct {
	// ID identifies this run.
	ID id.ID

	// ModelHash names the network that was screened.
	ModelHash string

	// SolverVersion is the solver that produced the screen.
	SolverVersion string

	// Candidates are every contingency's screen result, in the order they were
	// evaluated. Ranking is a separate operation, so the record preserves what
	// was asked rather than what was interesting.
	Candidates []Candidate

	// StartedAt and FinishedAt bound the run.
	StartedAt  time.Time
	FinishedAt time.Time
}

// Validate reports whether a run is coherent enough to store.
func (r Run) Validate() error {
	if r.ID.IsZero() {
		return fmt.Errorf("%w: a run needs an identifier", bserr.ErrInvalid)
	}

	if r.ModelHash == "" {
		return fmt.Errorf(
			"%w: a run must name the model it screened, or its results cannot "+
				"be reproduced", bserr.ErrInvalid)
	}

	if r.SolverVersion == "" {
		return fmt.Errorf(
			"%w: a run must name the solver version that produced it",
			bserr.ErrInvalid)
	}

	return nil
}

// Ranked returns the candidates worst first, for choosing what to verify.
//
// Contingencies the screen could not evaluate sort ahead of every evaluated
// one. They are not safe — they are unknown, and a radial outage that islands
// part of the network is usually the most consequential thing on the list.
// Sorting them anywhere else buries exactly the cases that most need
// simulating.
func (r Run) Ranked() []Candidate {
	out := slices.Clone(r.Candidates)

	slices.SortStableFunc(out, func(left, right Candidate) int {
		// Unevaluated first, whatever else is true of either.
		if by := cmp.Compare(rankBucket(left), rankBucket(right)); by != 0 {
			return by
		}

		if left.WorstLoading != nil && right.WorstLoading != nil {
			if by := cmp.Compare(*right.WorstLoading, *left.WorstLoading); by != 0 {
				return by
			}
		}

		// Ties break on branch index, which is canonical, so a ranking reads
		// the same way every run instead of depending on the sort.
		return cmp.Compare(left.Branch, right.Branch)
	})

	return out
}

// rankBucket puts unevaluated contingencies ahead of evaluated ones.
func rankBucket(c Candidate) int {
	if c.Screenability.Evaluated() {
		return 1
	}

	return 0
}

// Concerning returns the candidates that warrant verification, worst first.
func (r Run) Concerning() []Candidate {
	var out []Candidate

	for _, candidate := range r.Ranked() {
		if candidate.Concerning() {
			out = append(out, candidate)
		}
	}

	return out
}

// Store persists contingency runs.
//
// Append-only and not bitemporal, for the same reason solutions are: a run is
// a statement about what was computed at one instant, and a correction would
// be a claim that different numbers were produced.
type Store interface {
	// Create records a run.
	Create(ctx context.Context, in Run) (Run, error)

	// Get retrieves one by identifier.
	Get(ctx context.Context, runID id.ID) (Run, error)

	// ListByModel returns runs for one network, most recent first.
	ListByModel(ctx context.Context, modelHash string, limit int) ([]Run, error)
}
