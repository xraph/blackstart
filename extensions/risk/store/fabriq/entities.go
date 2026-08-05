package fabriq

import (
	"github.com/xraph/fabriq/core/registry"
)

// Contributor supplies this extension's entity specs to the composition root.
//
// Declared here, beside the model it names, rather than in the extension
// package — a spec whose Model field points at a struct three packages away
// is a spec nobody checks against its table.
type Contributor struct{}

// ExtensionName identifies the owner in registry errors.
func (Contributor) ExtensionName() string { return "risk" }

// Specs returns the entities this extension declares.
//
// One, and it projects to neither the graph nor search. A value row is a
// number about a component at a moment, not a node with edges worth
// traversing — the traversal question, "what weather cycle produced this
// pass", is answered by the computation entity a later task adds, not by this
// one.
func (Contributor) Specs() []registry.EntitySpec {
	return []registry.EntitySpec{
		{
			Name:  ValueEntity,
			Kind:  registry.KindAggregate,
			Model: (*valueModel)(nil),
		},
	}
}
