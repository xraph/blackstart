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
		// A model with no stored encoding is repaired rather than left broken.
		// These exist: models built before the byte plane was configured, and
		// models whose blob write failed after the row was written. The bytes
		// are identical by definition — the content hash matched — so
		// re-storing them cannot produce a different network, and refusing
		// would leave a model nothing can ever solve against with no way to
		// fix it short of deleting the row.
		if existing.BlobRef == "" {
			repaired, err := e.restore(ctx, existing, parsed.Model)
			if err != nil {
				return ImportResult{}, err
			}

			existing = repaired
		}

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

	reference, err := e.store.Blobs().Put(ctx, encoded)
	if err != nil {
		return ImportResult{}, fmt.Errorf("networkmodel: storing the encoding: %w", err)
	}

	name := in.Name
	if name == "" {
		name = parsed.Name
	}

	stored, err := e.store.Models().Create(ctx, model.NetworkModel{
		ID:               nmid.NewModel(),
		ContentHash:      hash,
		BlobRef:          reference,
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

	// Identity is written after the model row exists and before anything is
	// emitted, so no consumer of the import event can observe a model whose
	// branches are not there yet.
	branches := make([]model.Branch, 0, len(parsed.Model.Branches))

	for _, canonicalBranch := range parsed.Model.Branches {
		branches = append(branches, model.Branch{
			ID:      nmid.NewBranch(),
			ModelID: stored.ID,
			// Exactly the identifier the encoder writes and the solver echoes
			// back in a cascade event. Minting a separate numbering here would
			// produce rows nothing could look up.
			Ordinal: canonicalBranch.ID,
			FromBus: canonicalBranch.FromID,
			ToBus:   canonicalBranch.ToID,
			Alias:   parsed.Aliases[canonicalBranch.ID],
		})
	}

	if err := e.store.Branches().PutAll(ctx, stored.ID, branches); err != nil {
		return ImportResult{}, fmt.Errorf(
			"networkmodel: recording the branches of %s: %w", stored.ID, err)
	}

	e.emit(ctx, stored)

	return ImportResult{
		Model:            stored,
		DatasetVersionID: datasetID,
		Traceable:        traceable,
	}, nil
}

// restore re-stores a model's canonical encoding and records the reference.
//
// # Errors
//
// Returns an error when the bytes cannot be stored or the row cannot be
// updated. Both leave the model exactly as it was — unusable — rather than
// pointing at a blob that is not there.
func (e *Engine) restore(
	ctx context.Context, existing model.NetworkModel, parsed canonical.Model,
) (model.NetworkModel, error) {
	encoded, err := canonical.Encode(parsed)
	if err != nil {
		return model.NetworkModel{}, fmt.Errorf("networkmodel: encoding the model: %w", err)
	}

	reference, err := e.store.Blobs().Put(ctx, encoded)
	if err != nil {
		return model.NetworkModel{}, fmt.Errorf(
			"networkmodel: restoring the encoding for %s: %w", existing.ID, err)
	}

	updated, err := e.store.Models().SetBlobRef(ctx, existing.ID, reference)
	if err != nil {
		return model.NetworkModel{}, fmt.Errorf(
			"networkmodel: recording the restored encoding for %s: %w", existing.ID, err)
	}

	return updated, nil
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
//
// Named by content hash because that is what every caller holds: a result
// records the hash of the network it was computed against, never a storage
// reference. The model row is the one place that maps between them.
func (e *Engine) Encoding(ctx context.Context, hash string) ([]byte, error) {
	found, err := e.store.Models().FindByHash(ctx, hash)
	if err != nil {
		return nil, err
	}

	if found.BlobRef == "" {
		return nil, fmt.Errorf(
			"networkmodel: model %s has no stored encoding; it was built before "+
				"the byte plane was configured", found.ID)
	}

	return e.store.Blobs().Get(ctx, found.BlobRef)
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
