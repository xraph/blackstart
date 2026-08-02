package fabriq

import (
	"github.com/xraph/fabriq/core/registry"
)

// Contributor supplies this extension's entity specs to the composition root.
type Contributor struct{}

// ExtensionName identifies the owner in registry errors.
func (Contributor) ExtensionName() string { return "powerflow" }

// Specs returns the entities this extension declares.
//
// Not a graph node. A solution is an answer about a network, not a thing in
// it, and the walk that matters — from a result back to the model and dataset
// it came from — is provenance's job and already reified there.
func (Contributor) Specs() []registry.EntitySpec {
	return []registry.EntitySpec{
		{
			Name:  SolutionEntity,
			Kind:  registry.KindAggregate,
			Model: (*solutionRow)(nil),
		},
	}
}
