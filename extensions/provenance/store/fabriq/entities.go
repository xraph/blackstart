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
func (Contributor) ExtensionName() string { return "provenance" }

// Specs returns the entities this extension declares.
//
// This is the extension the graph exists for. geo declined to project its
// spans by pointing here: "the questions worth walking a graph for — what is
// connected to what, what a result was computed from — are answered by
// topology and provenance". This is the second of those, and the chain ADR
// 0006 promises is a traversal rather than four joins the caller assembles.
//
// Lineage is the interesting one. It is a reified edge — the row *is* the
// relationship — so it projects as one rather than as a node, the way scl's
// GOOSE subscription does. A node per edge would double the graph to support
// no query.
func (Contributor) Specs() []registry.EntitySpec {
	return []registry.EntitySpec{
		{
			Name:      DatasetEntity,
			Kind:      registry.KindAggregate,
			Model:     (*datasetModel)(nil),
			GraphNode: "DatasetVersion",
			Edges: []registry.EdgeSpec{
				{
					// Optional, and safely so: Fabriq's projector writes an
					// edge for a non-empty foreign key and removes one for an
					// empty value, so a dataset recorded without a registry
					// entry produces no edge rather than one pointing at
					// nothing.
					Field:  "source_id",
					Rel:    "DERIVED_FROM",
					Target: datasourceSourceEntity,
				},
			},
		},
		{
			Name:      ModelEntity,
			Kind:      registry.KindAggregate,
			Model:     (*modelModel)(nil),
			GraphNode: "ModelVersion",
		},
		{
			Name:  RunEntity,
			Kind:  registry.KindAggregate,
			Model: (*runModel)(nil),
			// ProvenanceRun rather than Run. Graph labels are global to the
			// registry, and a contingency sweep and a power-flow solve are
			// also runs — an unqualified label would be ambiguous the first
			// time either of them projects.
			GraphNode: "ProvenanceRun",
			Edges: []registry.EdgeSpec{
				{
					Field:  "model_version_id",
					Rel:    "COMPUTED_BY",
					Target: ModelEntity,
				},
			},
		},
		{
			Name:  LineageEntity,
			Kind:  registry.KindAggregate,
			Model: (*lineageModel)(nil),
			GraphEdge: &registry.GraphEdgeSpec{
				TypeField:   "relation",
				SourceField: "run_id",
				TargetField: "dataset_version_id",
				SourceLabel: "ProvenanceRun",
				TargetLabel: "DatasetVersion",
				// Carried onto the edge so a query can ask which inputs were
				// training data without joining back to the row — which is the
				// query that detects leakage, and therefore the one that must
				// not be awkward.
				PropFields: []string{"role", "recorded_at"},
			},
		},
	}
}
