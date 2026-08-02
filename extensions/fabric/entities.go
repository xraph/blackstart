package fabric

import (
	"github.com/xraph/fabriq/core/registry"

	fabriqdomain "github.com/xraph/fabriq/domain"
)

// Contributor supplies the entities Fabriq's own planes need.
//
// Only blob_object, not fabriq's whole domain pack. `domain.RegisterAll`
// registers site, asset, tag, link and fs_node alongside it — an example
// domain, complete with graph nodes and search indexes for entities blackstart
// does not model. Registering the pack to obtain one entity would put five
// invented ones in `bsctl entities`, in the graph, and in the migration
// surface.
type Contributor struct{}

// ExtensionName identifies the owner in registry errors.
func (Contributor) ExtensionName() string { return ExtensionName }

// Specs returns the entities this extension declares.
//
// blob_object is the catalog row Fabriq's content-addressable byte plane
// writes when bytes are stored: PutBlob puts the bytes through trove's CAS and
// then creates one of these through the command path, so a blob gets the same
// tenancy, outbox and audit treatment as any other write. Without the spec
// registered, PutBlob fails with "unknown entity type" — after the bytes are
// already on disk.
//
// Not a graph node. Many blob_objects may share a hash, the bytes are opaque,
// and nothing is answered by walking to one.
func (Contributor) Specs() []registry.EntitySpec {
	return []registry.EntitySpec{
		{
			Name:      "blob_object",
			Kind:      registry.KindAggregate,
			Model:     (*fabriqdomain.BlobObject)(nil),
			Subscribe: []registry.Scope{registry.ByID, registry.ByTenant},
		},
	}
}
