// Package id declares the cascade extension's identifier prefixes.
//
// Prefixes are registered with the central registry from init, so a collision
// with another extension stops the process at startup with both owners named
// rather than surfacing later as identifiers that parse as the wrong type.
package id

import "github.com/xraph/blackstart/pkg/id"

// Owner is the extension these prefixes belong to.
const Owner = "cascade"

// Run is one cascade simulation against one network model.
//
// Immutable once shipped: changing it invalidates every stored identifier
// that uses it and every provenance record naming one.
const Run id.Prefix = "casc"

//nolint:gochecknoinits // prefix registration is init-time by design
func init() {
	id.MustRegister(Run, Owner, "CascadeRun")
}

// NewRun mints a run identifier.
func NewRun() id.ID { return id.MustNew(Run) }

// ParseRun reads a run identifier, rejecting any other type.
func ParseRun(s string) (id.ID, error) { return id.ParseWithPrefix(s, Run) }
