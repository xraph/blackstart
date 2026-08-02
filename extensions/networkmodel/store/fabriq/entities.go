package fabriq

import (
	"github.com/xraph/fabriq/core/registry"
)

// Contributor supplies this extension's entity specs to the composition root.
//
// Declared here, beside the models they name, rather than in the extension
// package — a spec whose Model field points at a struct three packages away is
// a spec nobody checks against its table.
type Contributor struct{}

// ExtensionName identifies the owner in registry errors.
func (Contributor) ExtensionName() string { return "networkmodel" }

// Specs returns the entities this extension declares.
//
// A network model is projected as a graph node so that "what was solved
// against this network" can be walked rather than joined. The blob is not:
// megabytes of canonical encoding have no place in a graph, and projecting it
// would push the whole model through the outbox on every write.
func (Contributor) Specs() []registry.EntitySpec {
	return []registry.EntitySpec{
		{
			Name:      ModelEntity,
			Kind:      registry.KindAggregate,
			Model:     (*modelRow)(nil),
			GraphNode: "NetworkModel",
		},
		{
			Name:  BlobEntity,
			Kind:  registry.KindAggregate,
			Model: (*blobRow)(nil),
		},
	}
}
