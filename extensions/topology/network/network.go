// Package network models a power system as built.
//
// One authoritative node-breaker model, not two copies of the network. The
// bus-branch form a solver wants is derived from this by
// extensions/topology/reduce and is never stored as a second source of truth —
// two models of one network drift, and the drift is invisible until a result
// disagrees with a diagram.
//
// Three entities the plan's original list omitted are here because node-breaker
// topology is impossible without them: ConnectivityNode, Terminal and Bay.
// ExternalAlias is here too, from Phase 1 rather than Phase 2.5, because every
// importer needs "never discard the identifier the source used" and adding it
// later means re-importing everything to recover names that were thrown away.
package network

import (
	"context"
	"fmt"
	"strings"

	"github.com/xraph/blackstart/extensions/topology/reduce"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/store"
)

// Substation is a physical site.
type Substation struct {
	// ID identifies this substation.
	ID id.ID

	// Name is what people call it.
	Name string

	// Code is the operator's own identifier for the site, when there is one.
	Code string
}

// Validate reports whether a substation can be stored.
func (s Substation) Validate() error {
	if s.ID.IsZero() {
		return fmt.Errorf("%w: a substation needs an identifier", bserr.ErrInvalid)
	}

	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("%w: a substation needs a name", bserr.ErrInvalid)
	}

	return nil
}

// VoltageLevel is one nominal voltage within a substation.
//
// The boundary connectivity nodes never merge across: only a transformer
// connects two levels, and a transformer has impedance, so it survives the
// reduction as a branch rather than collapsing.
type VoltageLevel struct {
	// ID identifies this level.
	ID id.ID

	// SubstationID names the site.
	SubstationID id.ID

	// NominalKV is the nominal voltage in kilovolts.
	NominalKV float64

	// Name is what people call it: "230 kV", "Bus A".
	Name string
}

// Validate reports whether a voltage level can be stored.
func (v VoltageLevel) Validate() error {
	if v.ID.IsZero() {
		return fmt.Errorf("%w: a voltage level needs an identifier", bserr.ErrInvalid)
	}

	if v.SubstationID.IsZero() {
		return fmt.Errorf("%w: a voltage level must belong to a substation", bserr.ErrInvalid)
	}

	if v.NominalKV <= 0 {
		return fmt.Errorf(
			"%w: voltage level %s has nominal voltage %v kV; a level with no "+
				"voltage cannot be told apart from another, and the reduction "+
				"refuses to merge across levels",
			bserr.ErrInvalid, v.ID, v.NominalKV)
	}

	return nil
}

// Bay is a container for one circuit position.
//
// SCL has bays and they are the natural grouping, which is why this exists in
// Phase 1 rather than arriving with the SCL parser: adding a container later
// means re-parenting every piece of equipment under it.
type Bay struct {
	// ID identifies this bay.
	ID id.ID

	// VoltageLevelID names the level it sits in.
	VoltageLevelID id.ID

	// Name is what people call it.
	Name string
}

// ConnectivityNode is an electrical point.
type ConnectivityNode struct {
	// ID identifies this node.
	ID id.ID

	// VoltageLevelID names the level. Carried on the node rather than looked
	// up through its bay, because the reduction needs it on every node and a
	// node need not sit in a bay.
	VoltageLevelID id.ID

	// BayID names the containing bay, when there is one.
	BayID id.ID

	// Name is what the source model called it.
	Name string
}

// Validate reports whether a connectivity node can be stored.
func (c ConnectivityNode) Validate() error {
	if c.ID.IsZero() {
		return fmt.Errorf("%w: a connectivity node needs an identifier", bserr.ErrInvalid)
	}

	if c.VoltageLevelID.IsZero() {
		return fmt.Errorf(
			"%w: connectivity node %s names no voltage level; the reduction "+
				"refuses to merge across levels and cannot tell without one",
			bserr.ErrInvalid, c.ID)
	}

	return nil
}

// Terminal joins a piece of equipment to a connectivity node.
//
// A separate entity rather than a pair of columns on Equipment, because a
// transformer has three terminals and a busbar section has as many as there
// are things attached to it. Columns would cap the model at two.
type Terminal struct {
	// ID identifies this terminal.
	ID id.ID

	// EquipmentID names what is connected.
	EquipmentID id.ID

	// ConnectivityNodeID names where.
	ConnectivityNodeID id.ID

	// Sequence orders a device's terminals, so "the from end" is well defined
	// for a branch whose two ends are not interchangeable.
	Sequence int
}

// Equipment is one device.
type Equipment struct {
	// ID identifies this device.
	ID id.ID

	// Kind classifies it, using the same vocabulary the reduction speaks so a
	// snapshot needs no translation table.
	Kind reduce.Kind

	// VoltageLevelID names the level it sits in.
	VoltageLevelID id.ID

	// BayID names the containing bay, when there is one.
	BayID id.ID

	// Name is what the source model called it.
	Name string

	// Normally records the switch position the site is normally left in.
	//
	// Not the live position — that arrives from SCADA and belongs to a
	// snapshot, not to the model. Recording the normal state is what lets a
	// study run without a live feed, which is most studies.
	NormallyClosed bool
}

// Validate reports whether a piece of equipment can be stored.
func (e Equipment) Validate() error {
	if e.ID.IsZero() {
		return fmt.Errorf("%w: equipment needs an identifier", bserr.ErrInvalid)
	}

	if !e.Kind.Switchable() && !e.Kind.Impedance() &&
		e.Kind != reduce.KindGenerator && e.Kind != reduce.KindLoad {
		return fmt.Errorf(
			"%w: %q is not a kind of equipment this model recognizes",
			bserr.ErrInvalid, e.Kind)
	}

	if e.VoltageLevelID.IsZero() {
		return fmt.Errorf(
			"%w: equipment %s names no voltage level", bserr.ErrInvalid, e.ID)
	}

	return nil
}

// ExternalAlias records what a source model called something.
//
// An import never discards the identifier its source used. Without this, a
// re-import cannot recognize what it already has, a provenance trail cannot be
// followed back to the file it came from, and an operator searching for a line
// by the name on their diagram finds nothing.
type ExternalAlias struct {
	// ID identifies this alias.
	ID id.ID

	// EntityID names the blackstart entity.
	EntityID id.ID

	// Namespace says whose identifier this is: "matpower", "iec61850",
	// "activsg". Namespaced because two sources will use the same string for
	// different things.
	Namespace string

	// Value is the source's own identifier.
	Value string
}

// Validate reports whether an alias can be stored.
func (a ExternalAlias) Validate() error {
	if a.ID.IsZero() || a.EntityID.IsZero() {
		return fmt.Errorf("%w: an alias needs both identifiers", bserr.ErrInvalid)
	}

	if a.Namespace == "" {
		return fmt.Errorf(
			"%w: an alias needs a namespace; two sources will use the same "+
				"string for different things, and an unnamespaced alias cannot "+
				"say which one it came from", bserr.ErrInvalid)
	}

	if a.Value == "" {
		return fmt.Errorf("%w: an alias with no value records nothing", bserr.ErrInvalid)
	}

	return nil
}

// Filter narrows a listing.
//
// A declarative struct rather than SQL fragments, because it has to lower onto
// a DQL document later and a bag of strings cannot.
type Filter struct {
	// SubstationID restricts to one site. Zero matches all.
	SubstationID id.ID

	// VoltageLevelID restricts to one level. Zero matches all.
	VoltageLevelID id.ID

	// Kind restricts to one kind of equipment. Empty matches all.
	Kind reduce.Kind
}

// Store reads and writes the network model.
//
// The temporal context travels in ctx, so there is no AsOf variant of any
// method here — see pkg/bitemporal. A network is corrected constantly as
// surveys arrive, and knowing what the model said when a study ran is the
// whole reason this is bitemporal.
type Store interface {
	CreateSubstation(ctx context.Context, in Substation) (Substation, error)
	GetSubstation(ctx context.Context, want id.ID) (Substation, error)
	ListSubstations(ctx context.Context, page store.Page) ([]Substation, error)

	CreateVoltageLevel(ctx context.Context, in VoltageLevel) (VoltageLevel, error)
	ListVoltageLevels(ctx context.Context, f Filter) ([]VoltageLevel, error)

	CreateNode(ctx context.Context, in ConnectivityNode) (ConnectivityNode, error)
	ListNodes(ctx context.Context, f Filter) ([]ConnectivityNode, error)

	CreateEquipment(ctx context.Context, in Equipment) (Equipment, error)
	GetEquipment(ctx context.Context, want id.ID) (Equipment, error)
	ListEquipment(ctx context.Context, f Filter) ([]Equipment, error)

	CreateTerminal(ctx context.Context, in Terminal) (Terminal, error)
	ListTerminals(ctx context.Context, f Filter) ([]Terminal, error)

	CreateAlias(ctx context.Context, in ExternalAlias) (ExternalAlias, error)

	// ResolveAlias finds what a source model's identifier refers to, which is
	// how a re-import recognizes what it already has.
	ResolveAlias(ctx context.Context, namespace, value string) (ExternalAlias, error)
}
