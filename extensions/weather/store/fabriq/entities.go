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
func (Contributor) ExtensionName() string { return "weather" }

// Specs returns the entities this extension declares.
//
// Three, and none of the series tables. The hypertables are written by
// fabriq's Timeseries port, which takes a table name as a parameter rather
// than an entity — they carry no id or version column, so the registry would
// refuse them and be right to.
//
// A cycle is a graph node with an edge to its feed, which is what makes "which
// registry source produced this forecast" a traversal rather than a join. A
// grid is not: there are thousands per cycle and nothing asks a graph question
// about one. A station is not either, for the same reason geo's spans are not
// — it answers "where", and the graph answers "what is connected to what".
func (Contributor) Specs() []registry.EntitySpec {
	return []registry.EntitySpec{
		{
			Name:      CycleEntity,
			Kind:      registry.KindAggregate,
			Model:     (*cycleModel)(nil),
			GraphNode: "WeatherCycle",
			Edges: []registry.EdgeSpec{
				{
					Field:  "feed_id",
					Rel:    "FETCHED_FROM",
					Target: "datasource_feed",
				},
			},
		},
		{
			Name:  GridEntity,
			Kind:  registry.KindAggregate,
			Model: (*gridModel)(nil),
		},
		{
			Name:  StationEntity,
			Kind:  registry.KindAggregate,
			Model: (*stationModel)(nil),
		},
	}
}
