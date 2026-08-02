// Package topology owns the network as built.
//
// One authoritative node-breaker model. The bus-branch form a solver wants is
// derived here by extensions/topology/reduce and is never stored as a second
// source of truth — two models of one network drift, and the drift stays
// invisible until a result disagrees with a diagram.
//
// The engine's real job is assembling a snapshot: the reduction is pure and
// already tested against hand-built substations, so what remains to get wrong
// is reading the right rows and putting the terminals in the right order.
package topology

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/blackstart/extensions/topology/network"
	"github.com/xraph/blackstart/extensions/topology/reduce"
	"github.com/xraph/blackstart/extensions/topology/store"
	"github.com/xraph/blackstart/pkg/hooks"
	"github.com/xraph/blackstart/pkg/id"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

// Engine holds the extension's business logic.
type Engine struct {
	network network.Store
	bus     *hooks.Bus
}

// NewEngine builds the engine.
func NewEngine(s store.Store, bus *hooks.Bus) (*Engine, error) {
	if s == nil {
		return nil, errors.New("topology: engine needs a store")
	}

	return &Engine{network: s.Network(), bus: bus}, nil
}

// Overrides carry switch positions that differ from the normal state.
//
// Keyed by equipment identifier. Sparse rather than complete, because that is
// what a caller actually holds: a study asks "what if this one breaker were
// open", and requiring every position would make the caller restate the model
// to change one thing.
type Overrides map[id.ID]bool

// Reduce derives the bus-branch model for one substation.
//
// The switch positions come from each device's recorded normal state, with
// Overrides applied on top. Normal state rather than live state is the default
// because most studies run without a live feed, and defaulting to "unknown"
// would make the common case need a SCADA connection to answer at all.
//
// # Errors
//
// Returns an error when the substation cannot be read or the model it holds
// cannot be reduced — a terminal naming an absent node, or a switch with three
// ends.
func (e *Engine) Reduce(
	ctx context.Context, substation id.ID, overrides Overrides,
) (reduce.Reduction, error) {
	snapshot, states, err := e.Snapshot(ctx, substation)
	if err != nil {
		return reduce.Reduction{}, err
	}

	for equipment, closed := range overrides {
		key := equipment.String()

		if _, known := states[key]; !known {
			// Refused rather than ignored. An override naming equipment that is
			// not in this substation is a caller asking about a device that is
			// not there, and silently dropping it would answer a different
			// question than the one asked — with no sign that it had.
			return reduce.Reduction{}, fmt.Errorf(
				"topology: override names %s, which is not a switchable device "+
					"in substation %s", equipment, substation)
		}

		states[key] = closed
	}

	reduced, err := reduce.Reduce(snapshot, states)
	if err != nil {
		return reduce.Reduction{}, fmt.Errorf(
			"topology: reducing substation %s: %w", substation, err)
	}

	return reduced, nil
}

// Snapshot assembles one substation's node-breaker model and its normal
// switch positions.
//
// Exported because the reduction is not the only thing that wants a snapshot:
// a viewer draws one, and an SCL round-trip test compares one. Returning the
// states alongside keeps the two consistent — built from the same read rather
// than from two.
func (e *Engine) Snapshot(
	ctx context.Context, substation id.ID,
) (reduce.Snapshot, reduce.SwitchStates, error) {
	if substation.IsZero() {
		return reduce.Snapshot{}, nil, errors.New("topology: no substation named")
	}

	// Read the substation first, so a request for one that does not exist says
	// so rather than reporting an empty network — which is a different thing
	// and would read as a substation with nothing in it.
	if _, err := e.network.GetSubstation(ctx, substation); err != nil {
		return reduce.Snapshot{}, nil, err
	}

	filter := network.Filter{SubstationID: substation}

	nodes, err := e.network.ListNodes(ctx, filter)
	if err != nil {
		return reduce.Snapshot{}, nil, err
	}

	equipment, err := e.network.ListEquipment(ctx, filter)
	if err != nil {
		return reduce.Snapshot{}, nil, err
	}

	terminals, err := e.network.ListTerminals(ctx, filter)
	if err != nil {
		return reduce.Snapshot{}, nil, err
	}

	snapshot, states := assemble(nodes, equipment, terminals)

	return snapshot, states, nil
}

// assemble turns stored rows into the pure package's snapshot.
//
// Separate from the reads so it can be tested on rows rather than through a
// store, and because getting the terminal order wrong here is the failure this
// function exists to make visible: a line whose ends are swapped still solves.
func assemble(
	nodes []network.ConnectivityNode,
	equipment []network.Equipment,
	terminals []network.Terminal,
) (reduce.Snapshot, reduce.SwitchStates) {
	snapshot := reduce.Snapshot{
		Nodes:     make([]reduce.ConnectivityNode, 0, len(nodes)),
		Equipment: make([]reduce.Equipment, 0, len(equipment)),
	}

	for _, node := range nodes {
		snapshot.Nodes = append(snapshot.Nodes, reduce.ConnectivityNode{
			ID:             node.ID.String(),
			VoltageLevelID: node.VoltageLevelID.String(),
		})
	}

	// Terminals arrive ordered by equipment then sequence, so appending in
	// order yields each device's ends in the order the source model gave them.
	// That order is what makes "the from end" of a branch well defined, and a
	// branch with its ends swapped still solves — it just describes a
	// different network.
	byEquipment := make(map[id.ID][]string, len(equipment))
	for _, terminal := range terminals {
		byEquipment[terminal.EquipmentID] = append(
			byEquipment[terminal.EquipmentID], terminal.ConnectivityNodeID.String())
	}

	states := reduce.SwitchStates{}

	for _, item := range equipment {
		attached := byEquipment[item.ID]

		if len(attached) == 0 {
			// Skipped rather than refused. Equipment with no terminals is
			// connected to nothing, which is a real state during construction
			// and after a partial import — and it cannot affect a reduction it
			// takes no part in.
			continue
		}

		snapshot.Equipment = append(snapshot.Equipment, reduce.Equipment{
			ID:        item.ID.String(),
			Kind:      item.Kind,
			Terminals: attached,
		})

		if item.Kind.Switchable() {
			states[item.ID.String()] = item.NormallyClosed
		}
	}

	return snapshot, states
}

// Definition is a whole substation, written in one call.
//
// A whole site rather than one entity at a time, because a substation is only
// meaningful complete: a connectivity node with no equipment on it and a
// terminal whose node has not been written yet are both unreducible, and an
// importer writing entity-by-entity would leave the store in that state for as
// long as the import ran.
type Definition struct {
	// Substation is the site.
	Substation network.Substation

	// Levels are its voltage levels.
	Levels []network.VoltageLevel

	// Nodes are its connectivity nodes.
	Nodes []network.ConnectivityNode

	// Equipment is its devices.
	Equipment []network.Equipment

	// Terminals join equipment to nodes.
	Terminals []network.Terminal

	// Aliases record what the source model called each entity.
	Aliases []network.ExternalAlias
}

// Define writes a substation and everything in it.
//
// Ordered so that every reference is satisfiable when it is made: levels
// before the nodes and equipment that name them, both before the terminals
// that join them. The store checks a terminal's two ends exist, and that check
// is only useful if the writes arrive in an order where it can pass.
//
// # Errors
//
// Returns an error naming the entity that could not be written. Partial writes
// are possible and are not undone here — the fabriq store runs this inside a
// transaction and the in-memory one has nothing to roll back to.
func (e *Engine) Define(ctx context.Context, in Definition) (network.Substation, error) {
	site, err := e.network.CreateSubstation(ctx, in.Substation)
	if err != nil {
		return network.Substation{}, fmt.Errorf("topology: writing the substation: %w", err)
	}

	for _, level := range in.Levels {
		if _, err := e.network.CreateVoltageLevel(ctx, level); err != nil {
			return network.Substation{}, fmt.Errorf(
				"topology: writing voltage level %s: %w", level.ID, err)
		}
	}

	for _, node := range in.Nodes {
		if _, err := e.network.CreateNode(ctx, node); err != nil {
			return network.Substation{}, fmt.Errorf(
				"topology: writing connectivity node %s: %w", node.ID, err)
		}
	}

	for _, item := range in.Equipment {
		if _, err := e.network.CreateEquipment(ctx, item); err != nil {
			return network.Substation{}, fmt.Errorf(
				"topology: writing equipment %s: %w", item.ID, err)
		}
	}

	for _, terminal := range in.Terminals {
		if _, err := e.network.CreateTerminal(ctx, terminal); err != nil {
			return network.Substation{}, fmt.Errorf(
				"topology: writing terminal %s: %w", terminal.ID, err)
		}
	}

	for _, alias := range in.Aliases {
		if _, err := e.network.CreateAlias(ctx, alias); err != nil {
			return network.Substation{}, fmt.Errorf(
				"topology: writing alias %s/%s: %w", alias.Namespace, alias.Value, err)
		}
	}

	e.emit(ctx, site)

	return site, nil
}

// Substations lists the sites.
func (e *Engine) Substations(ctx context.Context, limit int) ([]network.Substation, error) {
	return e.network.ListSubstations(ctx, pkgstore.Page{Limit: limit})
}

// Substation reads one site.
func (e *Engine) Substation(ctx context.Context, want id.ID) (network.Substation, error) {
	return e.network.GetSubstation(ctx, want)
}

// Resolve finds what a source model's identifier refers to.
func (e *Engine) Resolve(
	ctx context.Context, namespace, value string,
) (network.ExternalAlias, error) {
	return e.network.ResolveAlias(ctx, namespace, value)
}

// emit publishes a domain event, best effort.
func (e *Engine) emit(ctx context.Context, site network.Substation) {
	if e.bus == nil {
		return
	}

	e.bus.Emit(ctx, hooks.Event{
		Name:      "topology.substation.defined",
		Extension: "topology",
		Entity:    "Substation",
		EntityID:  site.ID.String(),
		Op:        hooks.OpCreate,
		Payload:   site,
	})
}
