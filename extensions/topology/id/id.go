// Package id declares the topology extension's identifier prefixes.
//
// Prefixes are registered with the central registry from init, so a collision
// with another extension stops the process at startup with both owners named
// rather than surfacing later as identifiers that parse as the wrong type.
package id

import "github.com/xraph/blackstart/pkg/id"

// Owner is the extension these prefixes belong to.
const Owner = "topology"

// Identifier prefixes. Immutable once shipped: changing one invalidates every
// stored identifier that uses it, every external alias that resolved to one,
// and every provenance record naming one.
const (
	// Substation is a physical site.
	Substation id.Prefix = "sub"

	// VoltageLevel is one nominal voltage within a substation. Nodes never
	// merge across one, because only a transformer connects two.
	VoltageLevel id.Prefix = "vlev"

	// Bay is the container SCL uses and the natural grouping for a circuit
	// position. Present from Phase 1 because Phase 2.5 needs it and adding a
	// container later means re-parenting everything under it.
	Bay id.Prefix = "bay"

	// ConnectivityNode is an electrical point. The unit node-breaker topology
	// is expressed in, and the one the plan's entity list omitted — a
	// node-breaker model is impossible without it.
	ConnectivityNode id.Prefix = "cnode"

	// Terminal joins a piece of equipment to a connectivity node.
	Terminal id.Prefix = "term"

	// Equipment is a breaker, switch, line, transformer, generator or load.
	Equipment id.Prefix = "equip"

	// ExternalAlias records the identifier a source model used, so an import
	// never discards the name the source knew a thing by.
	ExternalAlias id.Prefix = "alias"
)

//nolint:gochecknoinits // prefix registration is init-time by design
func init() {
	for _, p := range []struct {
		prefix id.Prefix
		name   string
	}{
		{Substation, "Substation"},
		{VoltageLevel, "VoltageLevel"},
		{Bay, "Bay"},
		{ConnectivityNode, "ConnectivityNode"},
		{Terminal, "Terminal"},
		{Equipment, "Equipment"},
		{ExternalAlias, "ExternalAlias"},
	} {
		id.MustRegister(p.prefix, Owner, p.name)
	}
}

// NewSubstation mints a substation identifier.
func NewSubstation() id.ID { return id.MustNew(Substation) }

// NewVoltageLevel mints a voltage-level identifier.
func NewVoltageLevel() id.ID { return id.MustNew(VoltageLevel) }

// NewBay mints a bay identifier.
func NewBay() id.ID { return id.MustNew(Bay) }

// NewConnectivityNode mints a connectivity-node identifier.
func NewConnectivityNode() id.ID { return id.MustNew(ConnectivityNode) }

// NewTerminal mints a terminal identifier.
func NewTerminal() id.ID { return id.MustNew(Terminal) }

// NewEquipment mints an equipment identifier.
func NewEquipment() id.ID { return id.MustNew(Equipment) }

// NewExternalAlias mints an alias identifier.
func NewExternalAlias() id.ID { return id.MustNew(ExternalAlias) }

// ParseSubstation reads a substation identifier, rejecting any other type.
func ParseSubstation(s string) (id.ID, error) { return id.ParseWithPrefix(s, Substation) }

// ParseEquipment reads an equipment identifier, rejecting any other type.
func ParseEquipment(s string) (id.ID, error) { return id.ParseWithPrefix(s, Equipment) }
