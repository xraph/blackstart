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
//
// It names the network twice. The two are not a duplicate: one is the identity
// of what the solver was handed, the other the identity of the model as this
// platform stores it, and a record that carried only one could not answer both
// "which bytes produced these numbers" and "which stored network is this
// about". See the two fields.
type Run struct {
	// ID identifies this run.
	ID id.ID

	// ModelHash names the network as the SOLVER knows it: the hash of the
	// deterministic protobuf encoding that was uploaded, which is the key the
	// solver caches a model against. Provenance for the candidates below,
	// because it names the exact bytes the screen ran on.
	//
	// It keeps the plain name, and that is a decision rather than inertia.
	// This struct carries no json tags, so every field name here IS a
	// published wire key: the OpenAPI component "Run" declares "ModelHash",
	// web/packages/api-client/src/generated/codecs.ts maps that key to
	// modelHash, and web/apps/viewer/src/routes/runs.tsx renders it. Renaming
	// the field renames the key for a field whose meaning has not changed, and
	// powerflow.Solution.ModelHash is the same value under the same name — so
	// a rename here would leave the platform with two vocabularies for the
	// solver's hash instead of removing the ambiguity it was meant to remove.
	ModelHash string

	// CanonicalHash names the same network as the PLATFORM knows it:
	// canonical.Hash over networkmodel's canonical encoding. That is the hash
	// networkmodel stores a model under, the one bsctl prints, and the one a
	// scenario manifest pins — the hash a person actually has in hand, which
	// is why ListByModel below must accept it.
	//
	// A pointer because absent and empty are different, and here the
	// distinction is load-bearing rather than theoretical. Two runs have no
	// canonical hash at all: one recorded before this field existed, and one
	// screened against a model networkmodel never stored. Writing "" for
	// either would not record "unknown" — it would record a 64-character hash
	// that happens to be zero characters long, and ListByModel would then join
	// every such run into one network that does not exist.
	//
	// No optional:"true" here, unlike scn.ScenarioRun.SweepRunID, and the
	// difference is measured rather than assumed. Forge builds an OpenAPI
	// component's required array from omitempty and pointer-ness and knows
	// nothing of omitzero — so a non-pointer field relying on omitzero needs
	// the tag, and this one, being both a pointer and omitempty, is already
	// left out of required. The generated document was read back to confirm
	// it; see the spec test.
	//
	// The key matches the unmapped siblings above rather than switching to
	// snake_case. The generated client camelCases either form to the same
	// identifier, so snake_case would buy nothing and leave one field in the
	// component following a convention the other five do not.
	CanonicalHash *string `json:"CanonicalHash,omitempty"`

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

	// A present-but-empty canonical hash is a third state the field has no
	// meaning for, and the one that would do damage silently: nil says the
	// canonical hash is unknown, a real hash says which network, and "" says
	// neither while still comparing equal to itself in every ListByModel.
	if r.CanonicalHash != nil && *r.CanonicalHash == "" {
		return fmt.Errorf(
			"%w: a run's canonical hash is present and empty; leave it absent to "+
				"say it is unknown", bserr.ErrInvalid)
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
	//
	// The hash may be in EITHER space: a run matches when it equals the run's
	// ModelHash or its CanonicalHash. Both, because a caller has whichever one
	// it has — a user or a scenario manifest holds the canonical hash, while
	// every run recorded before CanonicalHash existed carries only the
	// solver's. Accepting only the canonical one would make those runs
	// unfindable by any string at all; accepting only the solver's is the gap
	// this pair was added to close.
	ListByModel(ctx context.Context, modelHash string, limit int) ([]Run, error)
}

// NamesModel reports whether a run names this network under either hash.
//
// Shared by both store implementations so the two cannot drift on what "this
// network" means — the memory store filters in Go and the Fabriq store in SQL,
// and a rule written twice is a rule that eventually says two things.
//
// A hash that matches either space identifies the network unambiguously: the
// two are sha256 over different encodings, so a value equal to one run's
// canonical hash and another run's protobuf hash would be a collision between
// two hash functions rather than an ambiguity this predicate could resolve.
func (r Run) NamesModel(hash string) bool {
	if r.ModelHash == hash {
		return true
	}

	return r.CanonicalHash != nil && *r.CanonicalHash == hash
}
