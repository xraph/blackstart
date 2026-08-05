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
// against this network" can be walked rather than joined.
//
// The canonical encoding is not an entity here at all any more. It lives in
// Fabriq's content-addressable byte plane, which declares its own blob_object
// catalog entity — so the bytes are deduplicated, ref-counted and audited
// without this extension owning a table of them.
func (Contributor) Specs() []registry.EntitySpec {
	return []registry.EntitySpec{
		{
			Name:      ModelEntity,
			Kind:      registry.KindAggregate,
			Model:     (*modelRow)(nil),
			GraphNode: "NetworkModel",
		},
		{
			Name:  BranchEntity,
			Kind:  registry.KindAggregate,
			Model: (*branchRow)(nil),
		},
		{
			Name:  PlacementEntity,
			Kind:  registry.KindAggregate,
			Model: (*placementRow)(nil),
		},
	}
}
