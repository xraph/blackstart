// Package run models one cascade simulation and its result.
//
// The wire's RPC is unary, unlike contingency's streamed screen: a cascade
// produces one figure — how it ended, what was lost, which islands survived —
// and there is no partial cascade worth reporting as it goes. So there is no
// candidate-versus-finding distinction here the way sweep.Candidate and a
// verification differ; a Run is the whole answer.
//
// Everything below is what the solver already computed. This package adds no
// physics to it — only the shape to store it in and the store interface to
// persist it through.
package run

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Outcome is how the cascade ended.
type Outcome string

const (
	// OutcomeSettled means every island reached a state that stopped
	// changing before the horizon or step cap.
	OutcomeSettled Outcome = "settled"

	// OutcomeTruncated means the horizon or step cap was reached with at
	// least one island still changing. Not a failure of the solver — a
	// cascade that genuinely ran longer than the budget allowed.
	OutcomeTruncated Outcome = "truncated"

	// OutcomeCollapsed means every island collapsed.
	OutcomeCollapsed Outcome = "collapsed"
)

// IslandCondition is the worst thing that happened to an island on the
// frequency axis, over the whole run — not the state at the final instant.
// An island that shed a quarter of its load and then recovered is
// ConditionShedding, not ConditionStable; collapsing to the final state would
// hide the most important thing that happened to it.
type IslandCondition string

const (
	ConditionStable     IslandCondition = "stable"
	ConditionRecovering IslandCondition = "recovering"
	ConditionShedding   IslandCondition = "shedding"
	ConditionCollapsed  IslandCondition = "collapsed"
)

// AcVerdict is what the AC solve on an island's final state found.
//
// Separate from IslandCondition deliberately: an island that is
// frequency-stable and voltage-insolvable is a real and important
// combination, and a single field would force it to be reported as only one
// of the two.
type AcVerdict string

const (
	VerdictSecure     AcVerdict = "secure"
	VerdictViolated   AcVerdict = "violated"
	VerdictInsolvable AcVerdict = "insolvable"
)

// EventKind is what kind of thing happened at one point in the cascade.
type EventKind string

const (
	EventOverload  EventKind = "overload"
	EventTrip      EventKind = "trip"
	EventIsland    EventKind = "island"
	EventShed      EventKind = "shed"
	EventStabilize EventKind = "stabilize"
)

// Event is one thing the simulator logged during the run.
type Event struct {
	// At is seconds since the initiating outage — simulated time, not a
	// quantity on the power-flow axis, so it is never divided by a base.
	At float64 `json:"at"`

	Kind    EventKind `json:"kind"`
	Element string    `json:"element"`
	Detail  string    `json:"detail"`

	// Island is which island this event belongs to, when the network had
	// already split by the time it happened.
	//
	// A pointer because absent and island zero are different: an event
	// before any split — an overload on the still-connected system, say —
	// is not island-specific, and reporting it against island 0 would claim
	// a split that had not happened yet.
	Island *uint32 `json:"island,omitempty"`
}

// Sample is one point on an island's frequency curve.
type Sample struct {
	// At is seconds from the start of the cascade, on the same clock as an
	// event's time — so a scrubber can put the curve and the log on one axis.
	At float64 `json:"at"`

	// Hz is absolute frequency, hertz. Carried across the wire undivided:
	// a frequency is not a power and has no per-unit base.
	Hz float64 `json:"hz"`
}

// Island is one island's state at the end of the run.
type Island struct {
	// Buses are the indices, into the model's bus list, that ended up on
	// this island.
	Buses []uint32 `json:"buses"`

	// BusCount is carried alongside Buses so a summary can report an
	// island's size without walking the slice, the same reason
	// model.NetworkModel carries BusCount beside its own bus list.
	BusCount int `json:"bus_count"`

	Condition IslandCondition `json:"condition"`
	Verdict   AcVerdict       `json:"verdict"`

	// Balance is generation minus load at the final instant, per unit.
	// Negative means short (load exceeds generation), positive means
	// surplus — the wire message IslandState.balance_w is the authority on
	// this sign; see its own comment.
	Balance float64 `json:"balance"`

	// Frequency is the settling value — the last instant this island had a
	// machine to report one from. Nadir is the worst value reached anywhere
	// in the run. Rocof is the largest-magnitude rate of change observed
	// anywhere in the run, signed.
	//
	// All three are HERTZ on the wire and stay hertz here. Dividing a
	// frequency by a power base is exactly the class of error the
	// SI-on-the-wire rule exists to prevent, and the result would look
	// entirely plausible in a report.
	//
	// Pointers because an island with no machine data — no inertia, no
	// governors — has no frequency to report, which is not the same as a
	// frequency of zero.
	Frequency *float64 `json:"frequency,omitempty"`
	Nadir     *float64 `json:"nadir,omitempty"`
	Rocof     *float64 `json:"rocof,omitempty"`

	// Shed is load shed on this island over the whole run, per unit.
	Shed float64 `json:"shed"`

	// Trace is the island's frequency curve over the run, in time order,
	// already decimated by the solver. Empty when no step observed a machine
	// on this island — an absent curve, not a flat line at zero.
	Trace []Sample `json:"trace,omitempty"`
}

// Run is one execution of a cascade simulation.
type Run struct {
	// ID identifies this run.
	ID id.ID `json:"id"`

	// ModelHash names the network that was simulated. Carried from the
	// solver's own response rather than recomputed here, because
	// SimulateCascadeResponse — unlike ScreenContingenciesResponse — states
	// it directly.
	ModelHash string `json:"model_hash"`

	// SolverVersion is the solver that produced the simulation, likewise
	// read from the response rather than configured into the engine.
	SolverVersion string `json:"solver_version"`

	// Outcome is how the cascade ended.
	Outcome Outcome `json:"outcome"`

	// Initiating names the outages that started the cascade, as branch
	// identifiers. An operator names lines, not indices.
	Initiating []string `json:"initiating"`

	// Events are every logged occurrence, in simulated-time order, as the
	// solver produced them. Reordering, if a client wants newest-first, is a
	// display concern and stays off the stored record.
	Events []Event `json:"events"`

	// Islands are the final state of every island the network split into,
	// or a single island covering the whole network if it never split.
	Islands []Island `json:"islands"`

	// LoadLost is total load shed across every island, per unit.
	LoadLost float64 `json:"load_lost"`

	// Steps is the number of discrete events simulated, for comparing
	// against whatever max_steps the request carried.
	Steps uint32 `json:"steps"`

	// Elapsed is SIMULATED time the cascade covered, SECONDS — the run's own
	// clock, not how long the solve took on the wall clock. Never divided by
	// a base. StartedAt/FinishedAt below are this side's own wall-clock
	// bounds on the request, which is a different question this field does
	// not answer.
	Elapsed float64 `json:"elapsed_s"`

	// StartedAt and FinishedAt bound the run, on this side of the wire: when
	// the request was issued and when the response was recorded.
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

// Validate reports whether a run is coherent enough to store.
func (r Run) Validate() error {
	if r.ID.IsZero() {
		return fmt.Errorf("%w: a run needs an identifier", bserr.ErrInvalid)
	}

	if r.ModelHash == "" {
		return fmt.Errorf(
			"%w: a run must name the model it simulated, or its result cannot "+
				"be reproduced", bserr.ErrInvalid)
	}

	if r.SolverVersion == "" {
		return fmt.Errorf(
			"%w: a run must name the solver version that produced it",
			bserr.ErrInvalid)
	}

	return nil
}

// Store persists cascade runs.
//
// Append-only and not bitemporal, for the same reason contingency's
// sweep.Store is: a run is a statement about what was simulated at one
// instant, and a correction would be a claim that different numbers were
// produced.
type Store interface {
	// Create records a run.
	Create(ctx context.Context, in Run) (Run, error)

	// Get retrieves one by identifier.
	Get(ctx context.Context, runID id.ID) (Run, error)

	// ListByModel returns runs for one network, most recent first.
	ListByModel(ctx context.Context, modelHash string, limit int) ([]Run, error)
}
