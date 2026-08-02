// Package solution models a solved power flow.
//
// A solution is a record of an answer, not a cache of one. It names the model
// that was solved, the method used, the solver that ran, and the operating
// point it was solved at — everything needed to reproduce the number, which
// spec §2.5 requires of every number this platform reports.
//
// The numerics live in Rust. Nothing here computes anything: this package is
// the domain shape those results take once they cross the boundary, and the
// project rule is normative — no power-systems mathematics in Go except unit
// conversion. A Go file computing an admittance is in the wrong language.
package solution

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Method is which power flow was run.
//
// A closed set, mirroring the contract's enum. It is spelled out here rather
// than reusing the generated protobuf type so the domain model does not depend
// on the wire format — the two change for different reasons and on different
// schedules.
type Method string

const (
	// MethodDC is the linearization: real power only, no voltages. What the
	// contingency screen uses.
	MethodDC Method = "dc"

	// MethodAC is the full nonlinear solve by Newton-Raphson.
	MethodAC Method = "ac"
)

// Valid reports whether a method is one this platform runs.
func (m Method) Valid() bool {
	return m == MethodDC || m == MethodAC
}

// Bus is one bus's result.
//
// The json tags are load-bearing rather than decorative: these are persisted
// as an encoded column, so a renamed field without a tag would silently change
// the stored format and make every existing solution unreadable.
//
// Per-unit, converted at the client boundary. The wire is SI and the domain is
// per unit, and this is the side of that conversion the rest of the platform
// sees.
type Bus struct {
	// VMagnitude is the voltage magnitude, per unit.
	VMagnitude float64 `json:"v_magnitude"`

	// VAngle is the voltage angle, radians.
	VAngle float64 `json:"v_angle"`

	// PInjection is net real power injection, per unit. At the slack this is
	// what the reference had to supply.
	PInjection float64 `json:"p_injection"`

	// QInjection is net reactive power injection, per unit.
	QInjection float64 `json:"q_injection"`
}

// Branch is one branch's result.
type Branch struct {
	// PFlow is real power at the from-end, per unit, positive from→to.
	PFlow float64 `json:"p_flow"`

	// Loading is flow magnitude over rating. Zero for an unrated branch,
	// because an unrated branch has an unknown limit rather than a zero one
	// and must never be reported as overloaded.
	Loading float64 `json:"loading"`
}

// Overloaded reports whether this branch exceeds the given loading threshold.
//
// A method rather than a field, because "overloaded" depends on a threshold
// the caller chooses — an emergency rating differs from a long-term one — and
// baking one in would assert a policy this package has no standing to assert.
func (b Branch) Overloaded(threshold float64) bool {
	return b.Loading > threshold
}

// Solution is a solved power flow and everything needed to reproduce it.
type Solution struct {
	// ID identifies this solution.
	ID id.ID

	// ModelHash names the network that was solved. The content hash, so the
	// network can be identified without storing a copy of it here.
	ModelHash string

	// Method is which power flow was run.
	Method Method

	// SolverVersion is the version of the solver that produced this. Part of
	// the reproducibility chain: a number is traceable to a dataset version, a
	// solver version and a code commit, and this is the middle link.
	SolverVersion string

	// EnforcedReactiveLimits records whether generator limits were applied.
	//
	// A limited and an unlimited solve answer different questions, so two
	// results that disagree are not necessarily inconsistent — but only if
	// this is recorded. Without it, the disagreement looks like a bug.
	EnforcedReactiveLimits bool

	// Buses and Branches are the answer, in the model's index order.
	Buses    []Bus
	Branches []Branch

	// Iterations is how many Newton steps it took. One for DC, which is a
	// single linear solve.
	//
	// Stored rather than logged: a change in convergence behavior that still
	// lands on the same answer is exactly the change nothing else notices.
	Iterations uint32

	// Residual is the largest bus mismatch at convergence, per unit.
	Residual float64

	// BindingGenerators are buses whose generators are pinned at a reactive
	// limit. Not violations, but the reason a voltage violation happened.
	BindingGenerators []uint32

	// SolvedAt is when the solve ran.
	SolvedAt time.Time
}

// Validate reports whether a solution is coherent enough to store.
//
// Checked on the way in rather than trusted, because a solution that names no
// model or no solver version is not reproducible, and storing it would put a
// number in the record that nothing can trace.
func (s Solution) Validate() error {
	if s.ID.IsZero() {
		return fmt.Errorf("%w: a solution needs an identifier", bserr.ErrInvalid)
	}

	if s.ModelHash == "" {
		return fmt.Errorf(
			"%w: a solution must name the model it solved, or the number it "+
				"carries cannot be reproduced", bserr.ErrInvalid)
	}

	if !s.Method.Valid() {
		return fmt.Errorf("%w: %q is not a power-flow method", bserr.ErrInvalid, s.Method)
	}

	if s.SolverVersion == "" {
		return fmt.Errorf(
			"%w: a solution must name the solver version that produced it",
			bserr.ErrInvalid)
	}

	if len(s.Buses) == 0 {
		return fmt.Errorf("%w: a solution with no buses solved nothing", bserr.ErrInvalid)
	}

	return nil
}

// Violations returns the branches loaded above a threshold, worst first.
//
// Returns indices rather than branches so a caller can name them from the
// model it holds. This package has no model and inventing branch names here
// would mean two sources of truth for what a branch is called.
func (s Solution) Violations(threshold float64) []int {
	var out []int

	for index, branch := range s.Branches {
		if branch.Overloaded(threshold) {
			out = append(out, index)
		}
	}

	// Worst first, so a caller that truncates keeps what matters. Ties break
	// on index, which is canonical, so the order is reproducible rather than
	// dependent on the sort implementation.
	slices.SortFunc(out, func(left, right int) int {
		if by := cmp.Compare(s.Branches[right].Loading, s.Branches[left].Loading); by != 0 {
			return by
		}

		return cmp.Compare(left, right)
	})

	return out
}

// Store persists solutions.
//
// Solutions are append-only and are not bitemporal. A solved power flow is a
// statement about one instant that was true when it was computed and does not
// become untrue later; correcting one would mean claiming a different number
// was produced, which is not a correction but a falsification. A re-solve
// against corrected inputs is a new solution that names the new model.
type Store interface {
	// Create records a solution.
	Create(ctx context.Context, in Solution) (Solution, error)

	// Get retrieves one by identifier.
	Get(ctx context.Context, solutionID id.ID) (Solution, error)

	// ListByModel returns solutions for one network, most recent first, so a
	// caller asking "what do we know about this model" gets the current answer
	// without paging.
	ListByModel(ctx context.Context, modelHash string, limit int) ([]Solution, error)
}
