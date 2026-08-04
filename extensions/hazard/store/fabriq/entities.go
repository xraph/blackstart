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
func (Contributor) ExtensionName() string { return "hazard" }

// Specs returns the entities this extension declares.
//
// Two, and not the series table. The hypertable is written by fabriq's
// Timeseries port, which takes a table name as a parameter rather than an
// entity — it carries no id or version column, so the registry would refuse it
// and be right to.
//
// A computation is a graph node with an edge to the provenance run it
// recorded, which is what makes "what weather was this rating conditioned on"
// a traversal rather than three joins. An assignment is not a node: it is a
// property of a branch, and the graph question about a branch is what it is
// connected to.
func (Contributor) Specs() []registry.EntitySpec {
	return []registry.EntitySpec{
		{
			Name:  AssignmentEntity,
			Kind:  registry.KindAggregate,
			Model: (*assignmentModel)(nil),
		},
		{
			Name:      ComputationEntity,
			Kind:      registry.KindAggregate,
			Model:     (*computationModel)(nil),
			GraphNode: "HazardComputation",
			Edges: []registry.EdgeSpec{
				{
					Field:  "cycle_id",
					Rel:    "COMPUTED_FROM",
					Target: "weather_cycle",
				},
			},
		},
	}
}
