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

// ParseRoute reads a route identifier, rejecting any other type.
func ParseRoute(s string) (id.ID, error) { return id.ParseWithPrefix(s, Route) }
