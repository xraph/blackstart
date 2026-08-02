package fabriq

import (
	"github.com/xraph/fabriq/core/registry"
)

// Contributor supplies this extension's entity specs to the composition root.
type Contributor struct{}

// ExtensionName identifies the owner in registry errors.
func (Contributor) ExtensionName() string { return "scl" }

// Specs returns the entities this extension declares.
//
// The subscription is the point of the whole extension. It is a reified edge —
// the row is the relationship — so Fabriq projects it into the graph as one,
// between two IED nodes. The result is two overlaid graphs maintained by a
// single write path: the electrical topology from one set of entities and the
// communication topology from this one, with no second pipeline to keep in
// step and no chance of the two disagreeing about what was written.
func (Contributor) Specs() []registry.EntitySpec {
	return []registry.EntitySpec{
		{
			Name:      IEDEntity,
			Kind:      registry.KindAggregate,
			Model:     (*iedRow)(nil),
			GraphNode: "IED",
			Search: registry.SearchSpec{
				Index:  "scl_ieds",
				Fields: []string{"name", "type", "manufacturer"},
			},
		},
		// Not a graph node, and therefore no edge to its device: fabriq
		// requires one for the other, and it is right to. A GOOSE stream is a
		// row belonging to a device, and the graph question that matters is
		// device-to-device — putting a node in the graph for every control
		// block would inflate it to support no query anyone asks.
		{
			Name:  PublicationEntity,
			Kind:  registry.KindAggregate,
			Model: (*publicationRow)(nil),
		},
		{
			Name:  SubscriptionEntity,
			Kind:  registry.KindAggregate,
			Model: (*subscriptionRow)(nil),
			GraphEdge: &registry.GraphEdgeSpec{
				TypeField:   "relation",
				SourceField: "subscriber_id",
				TargetField: "publisher_id",
				SourceLabel: "IED",
				TargetLabel: "IED",
				// Carried onto the edge so a query can ask for confirmed
				// subscriptions without joining back to the row. An unresolved
				// one is the subscriber's assertion, not a fact.
				PropFields: []string{"resolved", "publisher_cb", "data"},
			},
		},
	}
}
