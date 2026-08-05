// Package id declares the risk extension's identifier prefixes.
package id

import "github.com/xraph/blackstart/pkg/id"

// Owner is the extension these prefixes belong to.
const Owner = "risk"

// Identifier prefixes. Immutable once shipped.
const (
	// Computation is one pass of the risk model over a weather cycle.
	//
	// Components are not minted here. A conductor probability is about a span
	// and a structure probability about a structure, both of which already
	// exist with their own identifiers — this extension computes about things
	// rather than creating them.
	Computation id.Prefix = "rskcmp"
)

//nolint:gochecknoinits // prefix registration is init-time by design
func init() {
	for _, p := range []struct {
		prefix id.Prefix
		name   string
	}{
		{Computation, "Computation"},
	} {
		id.MustRegister(p.prefix, Owner, p.name)
	}
}

// NewComputation mints a computation identifier.
func NewComputation() id.ID { return id.MustNew(Computation) }

// ParseComputation reads a computation identifier, rejecting any other type.
func ParseComputation(s string) (id.ID, error) { return id.ParseWithPrefix(s, Computation) }
