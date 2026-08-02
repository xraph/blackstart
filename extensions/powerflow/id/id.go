// Package id declares the powerflow extension's identifier prefixes.
//
// Prefixes are registered with the central registry from init, so a collision
// with another extension stops the process at startup with both owners named
// rather than surfacing later as identifiers that parse as the wrong type.
package id

import "github.com/xraph/blackstart/pkg/id"

// Owner is the extension these prefixes belong to.
const Owner = "powerflow"

// Identifier prefixes. Immutable once shipped: changing one invalidates every
// stored identifier that uses it and every provenance record naming one.
const (
	// Solution is one solved power flow — a network model, a method, an
	// operating point, and the answer.
	Solution id.Prefix = "pfsol"
)

//nolint:gochecknoinits // prefix registration is init-time by design
func init() {
	id.MustRegister(Solution, Owner, "Solution")
}

// NewSolution mints a solution identifier.
func NewSolution() id.ID { return id.MustNew(Solution) }

// ParseSolution reads a solution identifier, rejecting any other type.
func ParseSolution(s string) (id.ID, error) { return id.ParseWithPrefix(s, Solution) }
