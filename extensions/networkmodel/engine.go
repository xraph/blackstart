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

		// Same repair, for the same reason, for a model whose branch or bus
		// write never landed: an interrupted first import that created the
		// model row and failed on Branches().PutAll or Buses().PutAll, or a
		// model built before those writes existed at all. Re-deriving the
		// rows from the bytes that hashed to this model cannot produce a
		// different network, and leaving it unrepaired would mean a
		// re-import of the exact same file returns AlreadyPresent forever
		// without ever fixing the model it points at.
		if err := e.repairIdentity(ctx, existing, parsed); err != nil {
			return ImportResult{}, err
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
	//
	// This write is not transactional with the Create above: if PutAll fails
	// here, the model row already exists with zero recorded branches, and
	// this call returns without emitting. That is not a silent gap — the
	// next import of these same bytes takes the AlreadyPresent path above,
	// which calls repairIdentity and writes the rows then. A model is never
	// left permanently branch-less by a failure here; it is left branch-less
	// until the next import attempt.
	if err := e.store.Branches().PutAll(ctx, stored.ID, branchRows(stored.ID, parsed)); err != nil {
		return ImportResult{}, fmt.Errorf(
			"networkmodel: recording the branches of %s: %w", stored.ID, err)
	}

	// Buses after branches, under the same non-transactional reasoning: an
	// interrupted import leaves a model with some identity rows and not
	// others, and the next import of these same bytes takes the
	// AlreadyPresent path, which calls repairIdentity and writes what is
	// missing.
	if err := e.store.Buses().PutAll(ctx, stored.ID, busRows(stored.ID, parsed)); err != nil {
		return ImportResult{}, fmt.Errorf(
			"networkmodel: recording the buses of %s: %w", stored.ID, err)
	}

	e.emit(ctx, stored)

	return ImportResult{
		Model:            stored,
		DatasetVersionID: datasetID,
		Traceable:        traceable,
	}, nil
}

// branchRows turns a parsed case's canonical branches into the rows an
// import records against one stored model.
//
// Shared by the fresh-import path and repairIdentity, so a branch minted
// during a repair is built exactly the same way as one minted during the
// original import — same ordinal source, same alias lookup.
func branchRows(modelID id.ID, parsed matpower.Case) []model.Branch {
	out := make([]model.Branch, 0, len(parsed.Model.Branches))

	for _, canonicalBranch := range parsed.Model.Branches {
		out = append(out, model.Branch{
			ID:      nmid.NewBranch(),
			ModelID: modelID,
			// Exactly the identifier the encoder writes and the solver echoes
			// back in a cascade event. Minting a separate numbering here would
			// produce rows nothing could look up.
			Ordinal: canonicalBranch.ID,
			FromBus: canonicalBranch.FromID,
			ToBus:   canonicalBranch.ToID,
			Alias:   parsed.Aliases[canonicalBranch.ID],
		})
	}

	return out
}

// busRows turns a parsed case's canonical buses into the rows an import
// records against one stored model.
//
// Shared by the fresh-import path and repairIdentity, so a bus minted during
// a repair is built exactly the same way as one minted during the original
// import — same ordinal source, same voltage.
func busRows(modelID id.ID, parsed matpower.Case) []model.Bus {
	out := make([]model.Bus, 0, len(parsed.Model.Buses))

	for _, canonicalBus := range parsed.Model.Buses {
		out = append(out, model.Bus{
			ID:      nmid.NewBus(),
			ModelID: modelID,
			// Exactly the identifier the encoder writes and Branch.FromBus
			// holds. Minting a separate numbering here would produce rows
			// nothing could look up.
			Ordinal: canonicalBus.ID,
			// Copied, not converted. Volts on both sides.
			VNominalV: canonicalBus.VNominalV,
		})
	}

	return out
}

// repairIdentity writes a model's branches and buses when a prior import
// recorded the model row but never recorded one or both.
//
// Follows restore's example for the analogous BlobRef gap: the bytes are
// identical by definition — FindByHash matched on content hash — so
// re-deriving the rows from this parse cannot describe a different network.
// A model whose BranchCount or BusCount says it has rows but whose store has
// none is exactly as unusable as one with no stored encoding, and left
// unrepaired it would keep returning AlreadyPresent forever with no route
// back to a working state.
//
// The two halves are checked independently, not one after the other with a
// shared early return: a model can have complete branches and zero buses —
// that is the state every model already in a database is in the moment this
// change ships, since bus rows only start being written by it — and an
// early return taken by the branch half must not skip the bus half.
//
// A model whose BranchCount, or whose BusCount, is genuinely zero is not
// touched on that half: a case with no mpc.branch rows, or the (nonexistent
// in practice) case with no mpc.bus rows, is not broken, it correctly has
// nothing to record there.
//
// Compared against BranchCount and BusCount, not against "any rows at all":
// PutAll is not transactional with the write that recorded those counts (see
// its own comment), so an interrupted import can leave some rows landed and
// some not. len(got) > 0 would read that partial state as already repaired
// and leave it that way forever — the exact state this function exists to
// fix, not a rarer cousin of the fully-empty case.
//
// The repair itself adds rows through AddMissing rather than replacing them
// through PutAll — which is a distinction that only matters here, because
// repairIdentity is the one caller for whom "short some rows" can mean rows
// already exist. PutAll deletes every row for the model before writing the
// set it is handed, and branchRows/busRows mint a fresh identifier for every
// row on every call, the same way they do for a brand-new import. Call PutAll
// here and a model that is short one branch out of two would not gain the
// missing one — it would lose the one it already had, get two new rows in
// its place, and hand the branch identifier a route or a hazard conductor
// assignment already resolved to nothing that resolves it any more. The rows
// still exist afterward; nothing has moved or been deleted from the model's
// perspective. But the identifier is gone, silently, and everything that
// named the old one is now naming a branch that does not exist. AddMissing
// is what the fully-empty and fully-complete cases at the top of this
// comment cannot distinguish from PutAll on their own: they have no existing
// identifier to lose, so a test built only from them would not catch this.
// The partial case is the one that would.
//
// # Errors
//
// Returns an error when the existing rows cannot be read or a repair cannot
// be written. Both leave the model exactly as it was — missing some or all
// of its branches or buses — rather than claiming a repair that did not
// happen.
func (e *Engine) repairIdentity(
	ctx context.Context, existing model.NetworkModel, parsed matpower.Case,
) error {
	if existing.BranchCount > 0 {
		got, err := e.store.Branches().ForModel(ctx, existing.ID)
		if err != nil {
			return fmt.Errorf(
				"networkmodel: checking %s for recorded branches: %w", existing.ID, err)
		}

		if len(got) < existing.BranchCount {
			if _, err := e.store.Branches().AddMissing(ctx, existing.ID, branchRows(existing.ID, parsed)); err != nil {
				return fmt.Errorf(
					"networkmodel: repairing the branches of %s: %w", existing.ID, err)
			}
		}
	}

	// Buses, compared against BusCount for the reason branches are compared
	// against BranchCount: PutAll is not transactional with the write that
	// recorded the count, so an interrupted import can leave some rows landed
	// and some not, and "any rows at all" would read that partial state as
	// already repaired and leave it that way forever.
	//
	// A model whose BusCount is genuinely zero is not touched. There is no
	// such MATPOWER case, but the branch half makes the same allowance and an
	// asymmetry here would be a puzzle for the next reader rather than a
	// safeguard.
	if existing.BusCount > 0 {
		heldBuses, err := e.store.Buses().ForModel(ctx, existing.ID)
		if err != nil {
			return fmt.Errorf(
				"networkmodel: checking %s for recorded buses: %w", existing.ID, err)
		}

		if len(heldBuses) < existing.BusCount {
			if _, err := e.store.Buses().AddMissing(ctx, existing.ID, busRows(existing.ID, parsed)); err != nil {
				return fmt.Errorf(
					"networkmodel: repairing the buses of %s: %w", existing.ID, err)
			}
		}
	}

	return nil
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

// Store exposes the composite for a caller that needs a domain this engine
// has no purpose-built method for — `bsctl model place` writes placements and
// `bsctl geo trace-model` reads branches and placements directly, and adding
// a pass-through method for each would just move the call up one layer.
//
// Not a general escape hatch: a caller reaching through it to write is
// bypassing whatever validation and hooks a purpose-built method would have
// applied.
func (e *Engine) Store() store.Store { return e.store }

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
