// Package provenance records what produced every number this platform reports.
//
// ADR 0006 makes the promise: a figure traces to the dataset version that fed
// it, the model or solver version that computed it, and the code commit that
// built the binary. This extension is where those are written and read.
//
// Other extensions take the Recorder interface rather than this engine, so that
// recording provenance does not couple them to this package's storage. That
// matters more than it looks: an extension that imports this one to record a
// run has just made provenance a build-time dependency of its own tests.
package provenance

import (
	"context"
	"errors"
	"fmt"
	"time"

	pvid "github.com/xraph/blackstart/extensions/provenance/id"
	"github.com/xraph/blackstart/extensions/provenance/record"
	"github.com/xraph/blackstart/extensions/provenance/store"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/hooks"
	"github.com/xraph/blackstart/pkg/id"
)

// Recorder is what other extensions depend on.
//
// Deliberately narrow, and deliberately an interface published here rather than
// a struct they import. An extension that needs to record a run should not gain
// the ability to rewrite the provenance graph, and should not need this
// package's storage compiled into its own tests.
type Recorder interface {
	// RecordDataset notes one retrieval, returning the existing record when
	// the same bytes have been seen before.
	RecordDataset(ctx context.Context, in record.DatasetVersion) (record.DatasetVersion, error)

	// RecordModel notes one producer version.
	RecordModel(ctx context.Context, in record.ModelVersion) (record.ModelVersion, error)

	// RecordRun notes one execution and the inputs it read.
	RecordRun(ctx context.Context, in record.ProvenanceRun, inputs []Input) (record.ProvenanceRun, error)
}

// Input is one dataset a run read, and how it was used.
type Input struct {
	// DatasetVersionID names the data.
	DatasetVersionID id.ID

	// Role is how it contributed. Input, training and validation are separate
	// because a backtest that reads training data has leaked, and one word for
	// all three would make that undetectable.
	Role record.Role
}

// Engine holds the extension's business logic.
type Engine struct {
	store store.Store
	bus   *hooks.Bus

	// now is injectable so a test can pin the times a record carries without
	// sleeping or comparing against a moving target.
	now func() time.Time
}

// NewEngine builds the engine.
//
// The bus may be nil — a CLI invocation has no interest in domain events.
func NewEngine(s store.Store, bus *hooks.Bus) (*Engine, error) {
	if s == nil {
		return nil, errors.New("provenance: engine needs a store")
	}

	return &Engine{store: s, bus: bus, now: time.Now}, nil
}

// RecordDataset notes one retrieval.
//
// Returns the existing record when the same content hash has been seen before,
// rather than creating a second version of identical bytes. That is what makes
// a re-download that changed nothing cheap: it does not invalidate every result
// derived from the previous retrieval, because there is no new version to
// derive from.
func (e *Engine) RecordDataset(
	ctx context.Context, in record.DatasetVersion,
) (record.DatasetVersion, error) {
	if in.ID.IsZero() {
		in.ID = pvid.NewDataset()
	}

	if in.RetrievedAt.IsZero() {
		in.RetrievedAt = e.now().UTC()
	}

	if in.ContentHash != "" {
		existing, err := e.store.Records().FindDatasetByHash(ctx, in.ContentHash)
		if err == nil {
			return existing, nil
		}

		// Anything other than "not found" is a real failure, and treating it as
		// absence would silently write a duplicate.
		if !errors.Is(err, bserr.ErrNotFound) {
			return record.DatasetVersion{}, fmt.Errorf(
				"provenance: checking for an existing retrieval: %w", err)
		}
	}

	stored, err := e.store.Records().CreateDataset(ctx, in)
	if err != nil {
		return record.DatasetVersion{}, err
	}

	e.emit(ctx, "provenance.dataset.recorded", "DatasetVersion", stored.ID)

	return stored, nil
}

// RecordModel notes one producer version.
func (e *Engine) RecordModel(
	ctx context.Context, in record.ModelVersion,
) (record.ModelVersion, error) {
	if in.ID.IsZero() {
		in.ID = pvid.NewModel()
	}

	if in.RecordedAt.IsZero() {
		in.RecordedAt = e.now().UTC()
	}

	stored, err := e.store.Records().CreateModel(ctx, in)
	if err != nil {
		return record.ModelVersion{}, err
	}

	e.emit(ctx, "provenance.model.recorded", "ModelVersion", stored.ID)

	return stored, nil
}

// RecordRun notes one execution and the inputs it read.
//
// The run and its lineage are written together, because a run recorded without
// its inputs is worse than no record: it looks traceable and is not. If an edge
// fails, the error names the run so the half-written chain can be found.
func (e *Engine) RecordRun(
	ctx context.Context, in record.ProvenanceRun, inputs []Input,
) (record.ProvenanceRun, error) {
	if in.ID.IsZero() {
		in.ID = pvid.NewRun()
	}

	if in.StartedAt.IsZero() {
		in.StartedAt = e.now().UTC()
	}

	if in.FinishedAt.IsZero() {
		in.FinishedAt = e.now().UTC()
	}

	stored, err := e.store.Records().CreateRun(ctx, in)
	if err != nil {
		return record.ProvenanceRun{}, err
	}

	for _, input := range inputs {
		edge := record.Lineage{
			ID:               pvid.NewLineage(),
			RunID:            stored.ID,
			DatasetVersionID: input.DatasetVersionID,
			Role:             input.Role,
			RecordedAt:       e.now().UTC(),
		}

		if _, err := e.store.Records().CreateLineage(ctx, edge); err != nil {
			return record.ProvenanceRun{}, fmt.Errorf(
				"provenance: run %s was recorded but its lineage to %s was not, "+
					"so its chain is incomplete: %w",
				stored.ID, input.DatasetVersionID, err)
		}
	}

	e.emit(ctx, "provenance.run.recorded", "Run", stored.ID)

	return stored, nil
}

// GetDataset retrieves one dataset version.
func (e *Engine) GetDataset(ctx context.Context, want id.ID) (record.DatasetVersion, error) {
	return e.store.Records().GetDataset(ctx, want)
}

// GetModel retrieves one producer version.
func (e *Engine) GetModel(ctx context.Context, want id.ID) (record.ModelVersion, error) {
	return e.store.Records().GetModel(ctx, want)
}

// GetRun retrieves one execution.
func (e *Engine) GetRun(ctx context.Context, want id.ID) (record.ProvenanceRun, error) {
	return e.store.Records().GetRun(ctx, want)
}

// Trace answers "what produced this number" in one call.
//
// The question ADR 0006 exists to make answerable mechanically rather than by
// investigation, so it is one method rather than three the caller assembles.
func (e *Engine) Trace(ctx context.Context, runID id.ID) (Trace, error) {
	run, err := e.store.Records().GetRun(ctx, runID)
	if err != nil {
		return Trace{}, err
	}

	producer, err := e.store.Records().GetModel(ctx, run.ModelVersionID)
	if err != nil {
		return Trace{}, fmt.Errorf(
			"provenance: run %s names model version %s, which is missing: %w",
			runID, run.ModelVersionID, err)
	}

	edges, err := e.store.Records().LineageOf(ctx, runID)
	if err != nil {
		return Trace{}, err
	}

	inputs := make([]TracedInput, 0, len(edges))

	for _, edge := range edges {
		dataset, err := e.store.Records().GetDataset(ctx, edge.DatasetVersionID)
		if err != nil {
			return Trace{}, fmt.Errorf(
				"provenance: run %s used dataset version %s, which is missing: %w",
				runID, edge.DatasetVersionID, err)
		}

		inputs = append(inputs, TracedInput{Role: edge.Role, Dataset: dataset})
	}

	return Trace{Run: run, Producer: producer, Inputs: inputs}, nil
}

// Trace is the full answer to "what produced this".
type Trace struct {
	// Run is the execution.
	Run record.ProvenanceRun

	// Producer is what computed it.
	Producer record.ModelVersion

	// Inputs are the datasets it read, with the role each played.
	Inputs []TracedInput
}

// TracedInput is one dataset a run read, and the role it played.
type TracedInput struct {
	// Role is how the dataset contributed. Serialized alongside the dataset
	// rather than as a separate list, so a consumer cannot pair them up wrong.
	Role record.Role `json:"role"`

	// Dataset is the retrieval.
	Dataset record.DatasetVersion `json:"dataset"`
}

// Reproducible reports whether this trace is complete enough to reproduce.
//
// A commit and a producer are required; inputs are not, because a run over a
// synthetic case reads no dataset and is still perfectly reproducible. What
// would make it irreproducible is not knowing which code ran.
func (t Trace) Reproducible() bool {
	return t.Producer.CodeCommit != "" && t.Run.Kind != ""
}

// emit publishes a domain event, best effort.
func (e *Engine) emit(ctx context.Context, name, entity string, entityID id.ID) {
	if e.bus == nil {
		return
	}

	e.bus.Emit(ctx, hooks.Event{
		Name:      name,
		Extension: "provenance",
		Entity:    entity,
		EntityID:  entityID.String(),
		Op:        hooks.OpCreate,
	})
}
