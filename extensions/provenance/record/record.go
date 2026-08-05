// Package record models what produced a number.
//
// ADR 0006 makes one promise: every figure this system reports is traceable to
// the dataset version that fed it, the model or solver version that computed
// it, and the code commit that built the binary. These are those three things,
// plus the edges that connect them.
//
// The reason this is Phase 0 work and not a later addition is in that ADR. The
// outage-prediction literature has a recurring reproducibility problem, and
// much of it comes from one mechanism: outage records are amended after the
// fact, so a model evaluated against the corrected record has seen information
// that did not exist when it claims to have predicted. Recording *which*
// version was read is what makes that detectable.
package record

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// DatasetVersion is one retrieval of one external dataset.
//
// A version, not a dataset: the same URL fetched twice is two versions if the
// bytes differ, and the same bytes fetched twice is one. That is what the
// content hash decides, and it is why a re-download that changed nothing does
// not invalidate every result derived from the previous one.
type DatasetVersion struct {
	// ID identifies this retrieval.
	ID id.ID

	// SourceID names the registry entry this came from, so a number traces
	// back to a license and a publisher rather than to a bare URL.
	SourceID id.ID

	// URL is where it was fetched from.
	URL string

	// ContentHash is the sha256 of what arrived, hex-encoded lowercase.
	//
	// The identity of the data. Two retrievals with the same hash are the same
	// version whatever their timestamps say, and two with different hashes are
	// different versions even if the publisher calls them the same release.
	ContentHash string

	// SizeBytes is how much arrived, recorded because a truncated download
	// that happens to parse is otherwise indistinguishable from a complete one.
	SizeBytes int64

	// ParserVersion is what read it. A dataset is only as reproducible as the
	// code that interpreted it, and a parser fix changes what the same bytes
	// mean.
	ParserVersion string

	// RetrievedAt is when it was fetched.
	RetrievedAt time.Time
}

// Validate reports whether a dataset version can be recorded.
func (d DatasetVersion) Validate() error {
	if d.ID.IsZero() {
		return fmt.Errorf("%w: a dataset version needs an identifier", bserr.ErrInvalid)
	}

	if d.URL == "" {
		return fmt.Errorf(
			"%w: a dataset version must say where it came from", bserr.ErrInvalid)
	}

	if _, err := url.Parse(d.URL); err != nil {
		return fmt.Errorf("%w: %q is not a URL: %w", bserr.ErrInvalid, d.URL, err)
	}

	if err := validateHash(d.ContentHash, "content hash"); err != nil {
		return err
	}

	if d.ParserVersion == "" {
		return fmt.Errorf(
			"%w: a dataset version must name the parser that read it; the same "+
				"bytes mean different things to different parsers", bserr.ErrInvalid)
	}

	if d.RetrievedAt.IsZero() {
		return fmt.Errorf(
			"%w: a dataset version must say when it was fetched", bserr.ErrInvalid)
	}

	return nil
}

// ModelVersion is one version of something that computes.
//
// A solver build, a trained model, a parser. Deliberately one type rather than
// three: they answer the same question — "what computed this" — and a result
// that has to say which of three tables its producer lives in is harder to
// query, not easier.
type ModelVersion struct {
	// ID identifies this version.
	ID id.ID

	// Kind classifies the producer.
	Kind ModelKind

	// Name identifies it within its kind: "bs-solver", "hrrr-parser".
	Name string

	// Version is the producer's own version string.
	Version string

	// CodeCommit is the commit the binary was built from.
	//
	// The third link in ADR 0006's chain. A version string alone does not
	// identify code — two builds can share one and differ — so this is what
	// makes "which code produced this" answerable.
	CodeCommit string

	// Parameters are the settings that change the answer: hyperparameters, a
	// solver tolerance, a seed. Recorded because a number is not reproducible
	// from a version alone if the same version can be run two ways.
	Parameters map[string]string

	// RecordedAt is when this version was first seen.
	RecordedAt time.Time
}

// ModelKind classifies what a version describes.
type ModelKind string

const (
	// KindSolver is a numerical solver build.
	KindSolver ModelKind = "solver"

	// KindParser is code that reads an external format.
	KindParser ModelKind = "parser"

	// KindModel is a trained statistical model.
	KindModel ModelKind = "model"
)

// Valid reports whether a kind is one this platform records.
func (k ModelKind) Valid() bool {
	return k == KindSolver || k == KindParser || k == KindModel
}

// Validate reports whether a model version can be recorded.
func (m ModelVersion) Validate() error {
	if m.ID.IsZero() {
		return fmt.Errorf("%w: a model version needs an identifier", bserr.ErrInvalid)
	}

	if !m.Kind.Valid() {
		return fmt.Errorf("%w: %q is not a model kind", bserr.ErrInvalid, m.Kind)
	}

	if m.Name == "" || m.Version == "" {
		return fmt.Errorf(
			"%w: a model version needs a name and a version", bserr.ErrInvalid)
	}

	if m.CodeCommit == "" {
		return fmt.Errorf(
			"%w: a model version must name the commit it was built from; a "+
				"version string alone does not identify code, since two builds "+
				"can share one and differ", bserr.ErrInvalid)
	}

	if m.RecordedAt.IsZero() {
		return fmt.Errorf("%w: a model version needs a timestamp", bserr.ErrInvalid)
	}

	return nil
}

// ProvenanceRun is one execution that produced a result.
//
// Named ProvenanceRun rather than the bare Run this package once used: Forge
// keys an OpenAPI component by Go type, not by the label passed to
// WithResponseSchema, so this collided with contingency's sweep.Run under one
// "Run" schema regardless of what either endpoint's registration called it —
// the same defect already fixed once for cascade's run.Run, in 2580898.
// Graph labels have carried the equivalent "ProvenanceRun" disambiguation
// since store/fabriq/entities.go's GraphNode/SourceLabel/TargetLabel fields,
// for the identical reason: a contingency sweep and a power-flow solve are
// also runs, so an unqualified name is ambiguous the first time either of
// them projects too. This rename makes the Go type say what the graph
// already did.
type ProvenanceRun struct {
	// ID identifies this run.
	ID id.ID

	// ModelVersionID names what computed it.
	ModelVersionID id.ID

	// Kind describes what was run: "powerflow", "contingency", "import".
	Kind string

	// ResultHash is the sha256 of the result, hex-encoded lowercase.
	//
	// What makes a claim of reproducibility checkable rather than asserted:
	// re-run from the same inputs and compare, and a mismatch is a finding
	// rather than a discussion.
	ResultHash string

	// Seed is the random seed, when there was one. Empty for a deterministic
	// computation, and recorded as such rather than as a zero — a zero seed is
	// a real seed.
	Seed string

	// StartedAt and FinishedAt bound the run.
	StartedAt  time.Time
	FinishedAt time.Time
}

// Validate reports whether a run can be recorded.
func (r ProvenanceRun) Validate() error {
	if r.ID.IsZero() {
		return fmt.Errorf("%w: a run needs an identifier", bserr.ErrInvalid)
	}

	if r.ModelVersionID.IsZero() {
		return fmt.Errorf(
			"%w: a run must name what computed it, or its result cannot be "+
				"reproduced", bserr.ErrInvalid)
	}

	if r.Kind == "" {
		return fmt.Errorf("%w: a run must say what kind of run it was", bserr.ErrInvalid)
	}

	if r.ResultHash != "" {
		if err := validateHash(r.ResultHash, "result hash"); err != nil {
			return err
		}
	}

	if r.StartedAt.IsZero() {
		return fmt.Errorf("%w: a run needs a start time", bserr.ErrInvalid)
	}

	return nil
}

// Role is how an input contributed to a run.
type Role string

const (
	// RoleInput is data the run read.
	RoleInput Role = "input"

	// RoleTraining is data a model was fitted on. Distinguished from input
	// because a backtest that reads training data has leaked, and the two
	// being one word would make that undetectable.
	RoleTraining Role = "training"

	// RoleValidation is data a model was scored against.
	RoleValidation Role = "validation"
)

// Valid reports whether a role is one this platform records.
func (r Role) Valid() bool {
	return r == RoleInput || r == RoleTraining || r == RoleValidation
}

// Lineage is a reified edge: this run used that dataset, in this role.
//
// An entity rather than a foreign key because the edge carries its own facts.
// Which role an input played is a property of the relationship, not of either
// end, and a run that used one dataset for training and another for validation
// cannot say so with a field on either side.
type Lineage struct {
	// ID identifies this edge.
	ID id.ID

	// RunID and DatasetVersionID are the ends.
	RunID            id.ID
	DatasetVersionID id.ID

	// Role is how the dataset contributed.
	Role Role

	// RecordedAt is when the dependency was noted.
	RecordedAt time.Time
}

// Validate reports whether a lineage edge can be recorded.
func (l Lineage) Validate() error {
	if l.ID.IsZero() {
		return fmt.Errorf("%w: a lineage edge needs an identifier", bserr.ErrInvalid)
	}

	if l.RunID.IsZero() || l.DatasetVersionID.IsZero() {
		return fmt.Errorf("%w: a lineage edge needs both ends", bserr.ErrInvalid)
	}

	if !l.Role.Valid() {
		return fmt.Errorf(
			"%w: %q is not a lineage role; input, training and validation are "+
				"distinguished because a backtest reading training data has "+
				"leaked, and one word for all three would hide it",
			bserr.ErrInvalid, l.Role)
	}

	if l.RecordedAt.IsZero() {
		return fmt.Errorf("%w: a lineage edge needs a timestamp", bserr.ErrInvalid)
	}

	return nil
}

// validateHash checks a hex-encoded sha256.
//
// Length and alphabet, not just emptiness. A truncated or upper-case hash
// compares unequal against a correct one, so a value that is nearly right is
// worse than one that is obviously missing.
func validateHash(value, field string) error {
	const sha256Hex = 64

	if value == "" {
		return fmt.Errorf("%w: a %s is required", bserr.ErrInvalid, field)
	}

	if len(value) != sha256Hex {
		return fmt.Errorf(
			"%w: %s %q is %d characters; a hex sha256 is %d",
			bserr.ErrInvalid, field, value, len(value), sha256Hex)
	}

	if strings.ToLower(value) != value {
		return fmt.Errorf(
			"%w: %s %q must be lower case, or it will not compare equal to one "+
				"that is", bserr.ErrInvalid, field, value)
	}

	for _, r := range value {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return fmt.Errorf(
				"%w: %s %q contains %q, which is not hexadecimal",
				bserr.ErrInvalid, field, value, r)
		}
	}

	return nil
}

// Store persists provenance records.
//
// Append-only and not bitemporal. A provenance record states what happened;
// correcting one would mean claiming a different thing happened, which is
// falsification rather than correction. If a record is wrong, the fix is a new
// record and an explanation, not an edit.
type Store interface {
	CreateDataset(ctx context.Context, in DatasetVersion) (DatasetVersion, error)
	GetDataset(ctx context.Context, want id.ID) (DatasetVersion, error)
	FindDatasetByHash(ctx context.Context, contentHash string) (DatasetVersion, error)

	CreateModel(ctx context.Context, in ModelVersion) (ModelVersion, error)
	GetModel(ctx context.Context, want id.ID) (ModelVersion, error)

	CreateRun(ctx context.Context, in ProvenanceRun) (ProvenanceRun, error)
	GetRun(ctx context.Context, want id.ID) (ProvenanceRun, error)

	CreateLineage(ctx context.Context, in Lineage) (Lineage, error)

	// LineageOf returns every input a run used, so "what fed this number" is
	// one query rather than a graph walk the caller has to write.
	LineageOf(ctx context.Context, runID id.ID) ([]Lineage, error)
}
