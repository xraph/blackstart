// Package id declares the hazard extension's identifier prefixes.
package id

import "github.com/xraph/blackstart/pkg/id"

// Owner is the extension these prefixes belong to.
const Owner = "hazard"

// Identifier prefixes. Immutable once shipped.
const (
	// Conductor is one entry in the conductor library.
	//
	// An identifier rather than only a code, because a conductor will become
	// an asset entity when that extension exists, and a library entry that
	// already has an identity migrates rather than being re-minted.
	Conductor id.Prefix = "hzcnd"

	// Assignment binds a branch to a conductor.
	//
	// Its own entity rather than a column on the branch, because the binding
	// carries facts of its own — chiefly whether it was surveyed or assumed,
	// which is a property of the relationship and not of either end.
	Assignment id.Prefix = "hzasg"

	// Computation is one pass of the hazard models over one weather cycle.
	Computation id.Prefix = "hzcmp"
)

//nolint:gochecknoinits // prefix registration is init-time by design
func init() {
	for _, p := range []struct {
		prefix id.Prefix
		name   string
	}{
		{Conductor, "Conductor"},
		{Assignment, "Assignment"},
		{Computation, "Computation"},
	} {
		id.MustRegister(p.prefix, Owner, p.name)
	}
}

// NewConductor mints a conductor identifier.
func NewConductor() id.ID { return id.MustNew(Conductor) }

// NewAssignment mints an assignment identifier.
func NewAssignment() id.ID { return id.MustNew(Assignment) }

// NewComputation mints a computation identifier.
func NewComputation() id.ID { return id.MustNew(Computation) }

// ParseConductor reads a conductor identifier, rejecting any other type.
func ParseConductor(s string) (id.ID, error) { return id.ParseWithPrefix(s, Conductor) }

// ParseAssignment reads an assignment identifier, rejecting any other type.
func ParseAssignment(s string) (id.ID, error) { return id.ParseWithPrefix(s, Assignment) }

// ParseComputation reads a computation identifier, rejecting any other type.
func ParseComputation(s string) (id.ID, error) { return id.ParseWithPrefix(s, Computation) }
