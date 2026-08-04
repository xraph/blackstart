package fabriq

import (
	"github.com/xraph/fabriq/core/registry"
)

// Contributor supplies this extension's entity specs to the composition root.
//
// Declared here, beside the model it names, rather than in the extension
// package — a spec whose Model field points at a struct three packages away is
// a spec nobody checks against its table.
type Contributor struct{}

// ExtensionName identifies the owner in registry errors.
func (Contributor) ExtensionName() string { return "cascade" }

// Specs returns the entities this extension declares.
//
// Not a graph node, for the reason contingency's run is not: a run is an
// answer about a network rather than a thing in it, and the walk that matters
// — from a result back to the model and the data it came from — is
// provenance's job and already reified there.
func (Contributor) Specs() []registry.EntitySpec {
	return []registry.EntitySpec{
		{
			Name:  RunEntity,
			Kind:  registry.KindAggregate,
			Model: (*runRow)(nil),
		},
	}
}
