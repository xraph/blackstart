// Package reduce turns a node-breaker network into a bus-branch one.
//
// A substation as built is a node-breaker model: connectivity nodes joined by
// breakers and disconnect switches, most of which are closed most of the time.
// A power flow wants a bus-branch model, where every set of nodes joined by
// closed zero-impedance devices has collapsed into one bus. This package is
// that collapse, and nothing else.
//
// Deliberately pure — no store, no context, no I/O. The reduction is the piece
// most likely to be wrong in a way that produces a plausible answer, so it has
// to be testable against hand-built substations with no database in the way.
// The four configurations in the golden tests are real ones: a ring bus, a
// breaker-and-a-half diameter, double-bus-double-breaker, and a bus section
// with a tie breaker.
//
// Deterministic numbering is load-bearing rather than tidy. Bus numbers feed
// the solver's content hash, so a reduction that numbered the same network two
// ways would produce two hashes for one network and defeat the model cache
// entirely.
package reduce

import (
	"fmt"
	"slices"
	"strings"
)

// Kind classifies a piece of equipment.
type Kind string

const (
	// KindBreaker is a circuit breaker: switchable, and zero impedance when
	// closed.
	KindBreaker Kind = "breaker"

	// KindSwitch is a disconnect or isolator. Also switchable and zero
	// impedance, and separate from a breaker because only one of them can
	// interrupt fault current — a distinction the reduction ignores and a
	// protection study does not.
	KindSwitch Kind = "switch"

	// KindLine is a transmission line or cable: real impedance, so it survives
	// the reduction as a branch.
	KindLine Kind = "line"

	// KindTransformer is a transformer. Survives as a branch.
	KindTransformer Kind = "transformer"

	// KindGenerator injects power at one node.
	KindGenerator Kind = "generator"

	// KindLoad draws power at one node.
	KindLoad Kind = "load"
)

// Switchable reports whether this kind of equipment can be opened, and so
// whether its state decides what merges.
func (k Kind) Switchable() bool {
	return k == KindBreaker || k == KindSwitch
}

// Impedance reports whether this kind survives the reduction as a branch.
//
// The complement of Switchable for present purposes, but stated separately
// because they answer different questions and will diverge: a series reactor
// is neither switchable nor a bus merge, and a future model will need to say
// so without changing what Switchable means.
func (k Kind) Impedance() bool {
	return k == KindLine || k == KindTransformer
}

// ConnectivityNode is an electrical point inside a substation.
//
// The thing breakers connect. A node-breaker model has many more of these than
// a bus-branch model has buses, which is the whole reason this package exists.
type ConnectivityNode struct {
	// ID is stable and comes from the source model.
	ID string

	// VoltageLevelID names the voltage level this node sits in. Nodes at
	// different voltages never merge, whatever a switch says: a transformer
	// separates them and a transformer has impedance.
	VoltageLevelID string
}

// Terminal connects one end of a piece of equipment to a node.
type Terminal struct {
	// EquipmentID names what is connected.
	EquipmentID string

	// NodeID names where.
	NodeID string
}

// Equipment is one device in the substation.
type Equipment struct {
	// ID is stable and comes from the source model.
	ID string

	// Kind classifies it.
	Kind Kind

	// Terminals are its connection points, in the source model's order. A
	// switch or a line has two; a generator or a load has one.
	Terminals []string
}

// Snapshot is a node-breaker model at one moment.
type Snapshot struct {
	// Nodes are every connectivity node, in any order.
	Nodes []ConnectivityNode

	// Equipment is every device.
	Equipment []Equipment
}

// SwitchStates says which switchable devices are closed.
//
// A map rather than a field on Equipment, because the topology and its state
// change on different schedules: the substation is rebuilt rarely and the
// breakers move constantly. Keeping them separate is what lets one snapshot be
// reduced under many states, which is exactly what contingency analysis does.
type SwitchStates map[string]bool

// Closed reports whether a device is closed.
//
// An unknown device is treated as closed. That is the safe default for this
// direction: assuming open would silently split a substation into pieces and
// report islands that do not exist, which reads as a finding. Assuming closed
// merges nodes that may not be joined, which shows up as a bus with more
// equipment on it than expected — visible rather than alarming.
func (s SwitchStates) Closed(equipmentID string) bool {
	closed, known := s[equipmentID]

	return !known || closed
}

// Bus is one set of connectivity nodes that a closed path joins.
type Bus struct {
	// Number is the bus's position in the reduced model, from 1.
	//
	// Assigned deterministically. See Reduce.
	Number int

	// VoltageLevelID is the level every member node sits in.
	VoltageLevelID string

	// Nodes are the member connectivity nodes, sorted.
	Nodes []string
}

// Branch is one piece of equipment with impedance, between two buses.
type Branch struct {
	// EquipmentID names the device.
	EquipmentID string

	// Kind is what it is.
	Kind Kind

	// From and To are bus numbers.
	From int
	To   int
}

// Injection is a generator or load, attached to one bus.
type Injection struct {
	// EquipmentID names the device.
	EquipmentID string

	// Kind is generator or load.
	Kind Kind

	// Bus is where it attaches.
	Bus int
}

// Reduction is the bus-branch model.
type Reduction struct {
	// Buses are the merged nodes, numbered from 1.
	Buses []Bus

	// Branches are the impedance devices between them.
	Branches []Branch

	// Injections are the generators and loads.
	Injections []Injection

	// Islands groups bus numbers that are connected to each other and to
	// nothing else.
	//
	// De-energized islands are reported, never deleted. A substation section
	// isolated by open breakers is a real part of the network in a real state,
	// and dropping it would mean the reduction quietly disagrees with the
	// model it came from about how many buses exist.
	Islands [][]int
}

// BusOf returns the bus number a node belongs to, or zero.
func (r Reduction) BusOf(nodeID string) int {
	for _, bus := range r.Buses {
		if slices.Contains(bus.Nodes, nodeID) {
			return bus.Number
		}
	}

	return 0
}

// Error is a structural problem in a snapshot.
type Error struct {
	// Reason says what is wrong.
	Reason string

	// Subject names the offending element.
	Subject string
}

func (e Error) Error() string {
	if e.Subject == "" {
		return "reduce: " + e.Reason
	}

	return fmt.Sprintf("reduce: %s: %s", e.Subject, e.Reason)
}

// validate checks a snapshot describes a network before any merging happens.
//
// Before rather than during, because a dangling terminal discovered halfway
// through a union-find produces a partial reduction and a confusing message,
// where the same problem named up front points straight at the source model.
func (s Snapshot) validate() error {
	nodes := make(map[string]struct{}, len(s.Nodes))

	for _, node := range s.Nodes {
		if node.ID == "" {
			return Error{Reason: "a connectivity node has no identifier"}
		}

		if _, seen := nodes[node.ID]; seen {
			return Error{Subject: node.ID, Reason: "duplicate connectivity node"}
		}

		nodes[node.ID] = struct{}{}
	}

	seen := make(map[string]struct{}, len(s.Equipment))

	for _, item := range s.Equipment {
		if item.ID == "" {
			return Error{Reason: "a piece of equipment has no identifier"}
		}

		if _, dup := seen[item.ID]; dup {
			return Error{Subject: item.ID, Reason: "duplicate equipment"}
		}

		seen[item.ID] = struct{}{}

		if len(item.Terminals) == 0 {
			return Error{Subject: item.ID, Reason: "equipment with no terminals is connected to nothing"}
		}

		for _, terminal := range item.Terminals {
			if _, ok := nodes[terminal]; !ok {
				return Error{
					Subject: item.ID,
					Reason:  "terminal references connectivity node " + terminal + ", which does not exist",
				}
			}
		}

		if err := item.validateArity(); err != nil {
			return err
		}
	}

	return nil
}

// validateArity checks a device has the number of terminals its kind implies.
func (e Equipment) validateArity() error {
	switch {
	case e.Kind.Switchable() || e.Kind.Impedance():
		if len(e.Terminals) != 2 {
			return Error{
				Subject: e.ID,
				Reason: fmt.Sprintf(
					"a %s joins two nodes and this one has %d terminals",
					e.Kind, len(e.Terminals)),
			}
		}

	case e.Kind == KindGenerator || e.Kind == KindLoad:
		if len(e.Terminals) != 1 {
			return Error{
				Subject: e.ID,
				Reason: fmt.Sprintf(
					"a %s attaches at one node and this one has %d terminals",
					e.Kind, len(e.Terminals)),
			}
		}

	default:
		return Error{
			Subject: e.ID,
			Reason:  "unknown equipment kind " + string(e.Kind),
		}
	}

	return nil
}

// sortNodeIDs orders identifiers the way bus numbering depends on.
//
// Plain lexicographic order on the identifier. TypeIDs are chronologically
// sortable within a prefix, so this is stable across runs and across machines
// — which is what the content hash needs, and what a locale-aware or
// length-first comparison would quietly break.
func sortNodeIDs(ids []string) {
	slices.SortFunc(ids, strings.Compare)
}
