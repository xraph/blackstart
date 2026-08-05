package fabriq

import (
	"github.com/xraph/fabriq/core/registry"
)

// Contributor supplies this extension's entity specs to the composition root.
type Contributor struct{}

// ExtensionName identifies the owner in registry errors.
func (Contributor) ExtensionName() string { return "geo" }

// Specs returns the entities this extension declares.
//
// Neither is a graph node. Geometry answers "where", and the questions worth
// walking a graph for — what is connected to what, what a result was computed
// from — are answered by topology and provenance. Projecting a span would put
// hundreds of thousands of nodes in the graph to support no query.
func (Contributor) Specs() []registry.EntitySpec {
	return []registry.EntitySpec{
		{
			Name:  RouteEntity,
			Kind:  registry.KindAggregate,
			Model: (*routeRow)(nil),
		},
		{
			Name:  SpanEntity,
			Kind:  registry.KindAggregate,
			Model: (*spanRow)(nil),
		},
		{
			Name:  CircuitEntity,
			Kind:  registry.KindAggregate,
			Model: (*circuitRow)(nil),
		},
		{
			Name:  StructureEntity,
			Kind:  registry.KindAggregate,
			Model: (*structureRow)(nil),
		},
	}
}
