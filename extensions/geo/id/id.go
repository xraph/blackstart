// Package id declares the geo extension's identifier prefixes.
package id

import "github.com/xraph/blackstart/pkg/id"

// Owner is the extension these prefixes belong to.
const Owner = "geo"

// Identifier prefixes. Immutable once shipped.
const (
	// Route is a branch's path over the ground.
	Route id.Prefix = "rte"

	// Span is one segment of a route, between two structures. The unit a
	// conductor actually fails on.
	Span id.Prefix = "span"

	// Structure is a tower or pole.
	Structure id.Prefix = "strc"

	// Footprint is the area an entity occupies — a substation's fence line.
	Footprint id.Prefix = "geom"

	// Circuit is a transmission line somebody actually built.
	//
	// Distinct from a network model's branch, and deliberately so: a branch in
	// an ACTIVSg case is a number in a synthetic network, while a circuit is a
	// line that exists. Geometry imported from OpenStreetMap or a transmission
	// line layer carries a circuit; nothing electrical does.
	Circuit id.Prefix = "circ"
)

//nolint:gochecknoinits // prefix registration is init-time by design
func init() {
	for _, p := range []struct {
		prefix id.Prefix
		name   string
	}{
		{Route, "Route"},
		{Span, "Span"},
		{Structure, "Structure"},
		{Footprint, "Footprint"},
		{Circuit, "Circuit"},
	} {
		id.MustRegister(p.prefix, Owner, p.name)
	}
}

// NewRoute mints a route identifier.
func NewRoute() id.ID { return id.MustNew(Route) }

// NewSpan mints a span identifier.
func NewSpan() id.ID { return id.MustNew(Span) }

// NewStructure mints a structure identifier.
func NewStructure() id.ID { return id.MustNew(Structure) }

// NewFootprint mints a footprint identifier.
func NewFootprint() id.ID { return id.MustNew(Footprint) }

// NewCircuit mints a circuit identifier.
func NewCircuit() id.ID { return id.MustNew(Circuit) }

// ParseRoute reads a route identifier, rejecting any other type.
func ParseRoute(s string) (id.ID, error) { return id.ParseWithPrefix(s, Route) }

// ParseSpan reads a span identifier, rejecting any other type.
func ParseSpan(s string) (id.ID, error) { return id.ParseWithPrefix(s, Span) }

// ParseStructure reads a structure identifier, rejecting any other type.
func ParseStructure(s string) (id.ID, error) { return id.ParseWithPrefix(s, Structure) }

// ParseCircuit reads a circuit identifier, rejecting any other type.
func ParseCircuit(s string) (id.ID, error) { return id.ParseWithPrefix(s, Circuit) }

// IsCircuit reports whether an identifier names a real circuit rather than a
// network model's branch.
//
// The question anything electrical asks before it tries to solve. A circuit has
// no impedance and no ratings, so a contingency handed one has nothing to
// compute with — and should say that rather than fail as a missing branch.
func IsCircuit(want id.ID) bool { return want.Prefix() == Circuit }
