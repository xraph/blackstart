// Package networkmodel builds and stores immutable network snapshots.
//
// The import pipeline lives here, and it is the one place in the system that
// turns a file into something the solver can be asked about. That makes it the
// place where ADR 0006's chain has to be established: the dataset version for
// the bytes, the parser version that read them, and the model those produced.
// A model built without that chain is a network nobody can trace a number back
// to.
package networkmodel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/xraph/blackstart/extensions/networkmodel/canonical"
	nmid "github.com/xraph/blackstart/extensions/networkmodel/id"
	"github.com/xraph/blackstart/extensions/networkmodel/matpower"
	"github.com/xraph/blackstart/extensions/networkmodel/model"
	"github.com/xraph/blackstart/extensions/networkmodel/store"
	"github.com/xraph/blackstart/extensions/provenance"
	"github.com/xraph/blackstart/extensions/provenance/record"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/hooks"
	"github.com/xraph/blackstart/pkg/id"
)

// Engine holds the extension's business logic.
type Engine struct {
	store    store.Store
	recorder provenance.Recorder
	bus      *hooks.Bus

	now func() time.Time
}

// NewEngine builds the engine.
//
// The recorder may be nil, and that is a deliberate allowance rather than an
// oversight: `bsctl model build` run against a local file has no provenance
// store to write to, and refusing to work without one would make the tool
// unusable for the exploration it exists for. Import records what it can and
// says in the result what it could not.
func NewEngine(s store.Store, recorder provenance.Recorder, bus *hooks.Bus) (*Engine, error) {
	if s == nil {
		return nil, errors.New("networkmodel: engine needs a store")
	}

	return &Engine{store: s, recorder: recorder, bus: bus, now: time.Now}, nil
}

// ImportRequest is one file to read.
type ImportRequest struct {
	// Source is the file's contents.
	Source []byte

	// URL is where it came from, recorded in the provenance chain. A local
	// path is fine — what matters is that the record says where someone
	// looked, not that it is fetchable.
	URL string

	// Name is what to call the resulting model. Excluded from the content
	// hash, so renaming does not invalidate results.
	Name string

	// SourceID names the registry entry this came from, when it came from one.
	SourceID id.ID
}

// ImportResult is what an import produced.
type ImportResult struct {
	// Model is the stored snapshot.
	Model model.NetworkModel

	// AlreadyPresent says the same network was already known, so nothing new
	// was built. Reported rather than hidden: re-importing an unchanged file
	// is a normal thing to do and produces no new model, and a caller seeing a
	// fresh identifier every time would conclude otherwise.
	AlreadyPresent bool

	// DatasetVersionID names the provenance record for the bytes, or is zero
	// when no recorder was configured.
	DatasetVersionID id.ID

	// Traceable says whether the chain from this model back to its source was
	// recorded. False means the model is usable and its numbers are not
	// reproducible from the record alone — stated rather than left for a
	// caller to infer from a zero identifier.
	Traceable bool
}

// Import reads a case file, records its provenance, and stores the model.
//
// # Errors
//
// Returns an error when the file cannot be read or the model cannot be stored.
// A failure to record provenance is *not* an error: the model is still correct
// and still useful, and refusing it would mean a provenance outage stops every
// import. The result says the chain is incomplete instead.
func (e *Engine) Import(ctx context.Context, in ImportRequest) (ImportResult, error) {
	if len(in.Source) == 0 {
		return ImportResult{}, errors.New("networkmodel: nothing to import")
	}

	if !matpower.LooksLikeCase(string(in.Source)) {
		return ImportResult{}, errors.New(
			"networkmodel: this does not look like a MATPOWER case; it has no " +
				"mpc.baseMVA. MATPOWER is the only format this build reads")
	}

	parsed, err := matpower.Parse(string(in.Source))
	if err != nil {
		return ImportResult{}, err
	}

	hash, err := canonical.Hash(parsed.Model)
	if err != nil {
		return ImportResult{}, fmt.Errorf("networkmodel: hashing the model: %w", err)
	}

	// The dataset version is recorded before the existing-model check, so a
	// re-import of an unchanged file still records that someone fetched it —
	// which is a fact about the world even when it produces no new model.
	datasetID, traceable := e.recordDataset(ctx, in)

	if existing, err := e.store.Models().FindByHash(ctx, hash); err == nil {
		return ImportResult{
			Model:            existing,
			AlreadyPresent:   true,
			DatasetVersionID: datasetID,
			Traceable:        traceable,
		}, nil
	} else if !errors.Is(err, bserr.ErrNotFound) {
		return ImportResult{}, fmt.Errorf(
			"networkmodel: checking for an existing model: %w", err)
	}

	encoded, err := canonical.Encode(parsed.Model)
	if err != nil {
		return ImportResult{}, fmt.Errorf("networkmodel: encoding the model: %w", err)
	}

	if err := e.store.Blobs().Put(ctx, hash, encoded); err != nil {
		return ImportResult{}, fmt.Errorf("networkmodel: storing the encoding: %w", err)
	}

	name := in.Name
	if name == "" {
		name = parsed.Name
	}

	stored, err := e.store.Models().Create(ctx, model.NetworkModel{
		ID:               nmid.NewModel(),
		ContentHash:      hash,
		Name:             name,
		SourceKind:       matpower.AliasNamespace,
		DatasetVersionID: datasetID,
		BusCount:         len(parsed.Model.Buses),
		BranchCount:      len(parsed.Model.Branches),
		BuiltAt:          e.now().UTC(),
	})
	if err != nil {
		return ImportResult{}, err
	}

	e.emit(ctx, stored)

	return ImportResult{
		Model:            stored,
		DatasetVersionID: datasetID,
		Traceable:        traceable,
	}, nil
}

// recordDataset writes the provenance for the bytes, reporting whether it
// managed to.
//
// Failure is survivable and is reported rather than raised. A provenance store
// that is down should not stop a model being built — but a model built without
// a chain must not claim to have one, which is what the returned flag carries.
func (e *Engine) recordDataset(ctx context.Context, in ImportRequest) (id.ID, bool) {
	if e.recorder == nil {
		return id.Nil, false
	}

	sum := sha256.Sum256(in.Source)

	stored, err := e.recorder.RecordDataset(ctx, record.DatasetVersion{
		SourceID:      in.SourceID,
		URL:           in.URL,
		ContentHash:   hex.EncodeToString(sum[:]),
		SizeBytes:     int64(len(in.Source)),
		ParserVersion: matpower.ParserVersion,
		RetrievedAt:   e.now().UTC(),
	})
	if err != nil {
		return id.Nil, false
	}

	return stored.ID, true
}

// Get retrieves a stored model.
func (e *Engine) Get(ctx context.Context, want id.ID) (model.NetworkModel, error) {
	return e.store.Models().Get(ctx, want)
}

// FindByHash answers "do we already have this network".
func (e *Engine) FindByHash(ctx context.Context, hash string) (model.NetworkModel, error) {
	return e.store.Models().FindByHash(ctx, hash)
}

// List returns models most recent first.
func (e *Engine) List(ctx context.Context, limit int) ([]model.NetworkModel, error) {
	return e.store.Models().List(ctx, limit)
}

// Encoding returns the canonical bytes a model encodes to.
//
// What the solver client uploads. Read from the blob store rather than
// re-encoded, so the bytes a result was computed from are the bytes that were
// stored — re-encoding would be correct today and would silently diverge the
// first time the encoder's version moved.
func (e *Engine) Encoding(ctx context.Context, hash string) ([]byte, error) {
	return e.store.Blobs().Get(ctx, hash)
}

// emit publishes a domain event, best effort.
func (e *Engine) emit(ctx context.Context, stored model.NetworkModel) {
	if e.bus == nil {
		return
	}

	e.bus.Emit(ctx, hooks.Event{
		Name:      "networkmodel.built",
		Extension: "networkmodel",
		Entity:    "NetworkModel",
		EntityID:  stored.ID.String(),
		Op:        hooks.OpCreate,
		At:        stored.BuiltAt,
		Payload:   stored,
	})
}
