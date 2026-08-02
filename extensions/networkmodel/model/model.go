// Package model records an immutable network snapshot.
//
// A NetworkModel is a snapshot and is deliberately *not* bitemporal.
// Bitemporality lives in what it points at — the topology, which is corrected
// as surveys arrive — and a snapshot of corrected data is a different snapshot
// rather than a correction of this one. Making the snapshot itself bitemporal
// would mean a model could change under a result that cites it.
package model

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// NetworkModel is one built snapshot of a network.
type NetworkModel struct {
	// ID identifies this snapshot.
	ID id.ID `json:"id"`

	// ContentHash is what the canonical encoder produced.
	//
	// The model's real identity. Two snapshots with one hash describe the same
	// network whatever else differs about them, which is what lets the solver
	// cache work and what lets a result name a network without carrying it.
	ContentHash string `json:"content_hash"`

	// Name is what to call it. Excluded from the hash on purpose: renaming a
	// model must not invalidate every result computed from it.
	Name string `json:"name"`

	// SourceKind says where it came from: "matpower", "topology", "scl".
	SourceKind string `json:"source_kind"`

	// BlobRef names the stored canonical encoding in the byte plane.
	//
	// Fabriq's content-addressable store mints this when the bytes are
	// written, and it is the only way back to them. Kept on the row rather
	// than derived from the content hash: the two agree today because both are
	// SHA-256 of the same bytes, and depending on that would break silently
	// the day either side changed its digest.
	BlobRef string `json:"blob_ref"`

	// DatasetVersionID names the provenance record for the file it was built
	// from, when there was one. Zero for a model assembled from the topology
	// store rather than imported.
	DatasetVersionID id.ID `json:"dataset_version_id"`

	// BusCount and BranchCount are carried so a listing can say how big a
	// model is without decoding it.
	BusCount    int `json:"bus_count"`
	BranchCount int `json:"branch_count"`

	// BuiltAt is when it was assembled.
	BuiltAt time.Time `json:"built_at"`
}

// Validate reports whether a model can be stored.
func (m NetworkModel) Validate() error {
	if m.ID.IsZero() {
		return fmt.Errorf("%w: a network model needs an identifier", bserr.ErrInvalid)
	}

	if len(m.ContentHash) != 64 || strings.ToLower(m.ContentHash) != m.ContentHash {
		return fmt.Errorf(
			"%w: content hash %q is not a lower-case hex sha256; a hash that is "+
				"nearly right compares unequal to one that is, which is worse "+
				"than one that is obviously missing",
			bserr.ErrInvalid, m.ContentHash)
	}

	if m.SourceKind == "" {
		return fmt.Errorf(
			"%w: a network model must say where it came from", bserr.ErrInvalid)
	}

	if m.BusCount <= 0 {
		return fmt.Errorf(
			"%w: a network model with no buses describes nothing", bserr.ErrInvalid)
	}

	return nil
}

// Store persists network models.
//
// Append-only. A model is a snapshot: correcting one would mean a result that
// cites it now refers to a different network, which is the failure this whole
// design exists to prevent.
type Store interface {
	Create(ctx context.Context, in NetworkModel) (NetworkModel, error)
	Get(ctx context.Context, want id.ID) (NetworkModel, error)

	// FindByHash answers "do we already have this network", which every import
	// asks before doing any work.
	FindByHash(ctx context.Context, contentHash string) (NetworkModel, error)

	// List returns models most recent first.
	List(ctx context.Context, limit int) ([]NetworkModel, error)

	// SetBlobRef records where a model's canonical encoding is stored.
	//
	// The one mutation this store allows, and narrow on purpose. Append-only
	// protects the network a model describes, because a result citing it must
	// keep meaning what it meant. Where the bytes live is not part of that:
	// the content hash is unchanged, the physics is unchanged, and only the
	// pointer to storage moves. A general Update would let the physics move
	// too, and nothing would notice.
	SetBlobRef(ctx context.Context, want id.ID, reference string) (NetworkModel, error)
}
