package fabriq

import (
	"github.com/xraph/fabriq/core/registry"
)

// Contributor supplies this extension's entity specs to the composition root.
//
// The specs are declared here, beside the models they name, rather than in the
// extension package — a spec whose Model field points at a struct three
// packages away is a spec nobody checks against its table.
type Contributor struct{}

// ExtensionName identifies the owner in registry errors.
func (Contributor) ExtensionName() string { return "datasource" }

// Specs returns the entities this extension declares.
//
// The edge is the point. A feed's source_id column is projected as a
// BELONGS_TO relationship, so one write through fabriq.Exec produces both the
// relational row and the graph edge. That is the architectural claim this
// extension exists to demonstrate, and the integration suite asserts it
// against a real FalkorDB rather than trusting the declaration.
func (Contributor) Specs() []registry.EntitySpec {
	return []registry.EntitySpec{
		{
			Name:      SourceEntity,
			Kind:      registry.KindAggregate,
			Model:     (*sourceModel)(nil),
			GraphNode: "DataSource",
		},
		{
			Name:      FeedEntity,
			Kind:      registry.KindAggregate,
			Model:     (*feedModel)(nil),
			GraphNode: "DataFeed",
			Edges: []registry.EdgeSpec{
				{
					Field:  "source_id",
					Rel:    "BELONGS_TO",
					Target: SourceEntity,
				},
			},
		},
	}
}
