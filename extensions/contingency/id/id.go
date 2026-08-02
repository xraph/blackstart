// Package id declares the contingency extension's identifier prefixes.
//
// Prefixes are registered with the central registry from init, so a collision
// with another extension stops the process at startup with both owners named
// rather than surfacing later as identifiers that parse as the wrong type.
package id

import "github.com/xraph/blackstart/pkg/id"

// Owner is the extension these prefixes belong to.
const Owner = "contingency"

// Identifier prefixes. Immutable once shipped: changing one invalidates every
// stored identifier that uses it and every provenance record naming one.
const (
	// Set is a named list of contingencies to evaluate — the ordering a risk
	// model eventually supplies.
	Set id.Prefix = "cset"

	// Run is one execution of a set against one network model.
	Run id.Prefix = "crun"
)

//nolint:gochecknoinits // prefix registration is init-time by design
func init() {
	id.MustRegister(Set, Owner, "ContingencySet")
	id.MustRegister(Run, Owner, "ContingencyRun")
}

// NewSet mints a contingency-set identifier.
func NewSet() id.ID { return id.MustNew(Set) }

// NewRun mints a run identifier.
func NewRun() id.ID { return id.MustNew(Run) }

// ParseSet reads a set identifier, rejecting any other type.
func ParseSet(s string) (id.ID, error) { return id.ParseWithPrefix(s, Set) }

// ParseRun reads a run identifier, rejecting any other type.
func ParseRun(s string) (id.ID, error) { return id.ParseWithPrefix(s, Run) }
