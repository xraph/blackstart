// Package id declares the provenance extension's identifier prefixes.
//
// Prefixes are registered with the central registry from init, so a collision
// with another extension stops the process at startup with both owners named
// rather than surfacing later as identifiers that parse as the wrong type.
package id

import "github.com/xraph/blackstart/pkg/id"

// Owner is the extension these prefixes belong to.
const Owner = "provenance"

// Identifier prefixes. Immutable once shipped: changing one invalidates every
// stored identifier that uses it and every record naming one.
const (
	// Dataset is one retrieval of one external dataset — a URL, a moment, a
	// content hash.
	Dataset id.Prefix = "dsv"

	// Model is one version of something that computes: a solver build, a
	// trained model, a parser.
	Model id.Prefix = "mver"

	// Run is one execution that produced a result.
	Run id.Prefix = "run"

	// Lineage is a reified edge: this run used that dataset. An entity rather
	// than a field because an edge carries its own facts — which role the
	// input played, and when the dependency was recorded.
	Lineage id.Prefix = "lnge"
)

//nolint:gochecknoinits // prefix registration is init-time by design
func init() {
	id.MustRegister(Dataset, Owner, "DatasetVersion")
	id.MustRegister(Model, Owner, "ModelVersion")
	id.MustRegister(Run, Owner, "Run")
	id.MustRegister(Lineage, Owner, "Lineage")
}

// NewDataset mints a dataset-version identifier.
func NewDataset() id.ID { return id.MustNew(Dataset) }

// NewModel mints a model-version identifier.
func NewModel() id.ID { return id.MustNew(Model) }

// NewRun mints a run identifier.
func NewRun() id.ID { return id.MustNew(Run) }

// NewLineage mints a lineage identifier.
func NewLineage() id.ID { return id.MustNew(Lineage) }

// ParseDataset reads a dataset-version identifier, rejecting any other type.
func ParseDataset(s string) (id.ID, error) { return id.ParseWithPrefix(s, Dataset) }

// ParseModel reads a model-version identifier, rejecting any other type.
func ParseModel(s string) (id.ID, error) { return id.ParseWithPrefix(s, Model) }

// ParseRun reads a run identifier, rejecting any other type.
func ParseRun(s string) (id.ID, error) { return id.ParseWithPrefix(s, Run) }
