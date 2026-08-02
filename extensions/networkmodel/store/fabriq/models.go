// Package fabriq is the Fabriq-backed implementation of the network-model
// store.
//
// Models here are unexported and per-backend, with explicit mappers to and
// from the domain types. The domain structs carry no grove tags: keeping the
// persistence shape separate is what lets the column layout change without
// touching the type the rest of the platform passes around.
package fabriq

import (
	"time"

	"github.com/xraph/grove"

	"github.com/xraph/blackstart/extensions/networkmodel/model"
	"github.com/xraph/blackstart/pkg/id"
)

// Table names, prefixed with the extension so a shared schema stays legible.
const (
	ModelTable = "blackstart_networkmodel_model"
	BlobTable  = "blackstart_networkmodel_blob"
)

// Entity names as registered with Fabriq.
const (
	ModelEntity = "networkmodel_model"
	BlobEntity  = "networkmodel_blob"
)

// modelRow is the persistence shape of a network model.
//
// The first three fields are Fabriq's structural contract: every entity
// declares id, tenant_id and version, and the registry refuses a model that
// does not. They are stamped by the executor, so caller values are ignored.
type modelRow struct {
	grove.BaseModel `grove:"table:blackstart_networkmodel_model"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	ContentHash      string    `grove:"content_hash,notnull"`
	Name             string    `grove:"name"`
	SourceKind       string    `grove:"source_kind,notnull"`
	DatasetVersionID string    `grove:"dataset_version_id"`
	BusCount         int       `grove:"bus_count,notnull"`
	BranchCount      int       `grove:"branch_count,notnull"`
	BuiltAt          time.Time `grove:"built_at,notnull"`
}

// blobRow is the canonical encoding of a model.
//
// A separate entity because the two have different shapes and lifetimes: the
// record is small and queried constantly, the blob is megabytes and read only
// when a solve needs uploading. One row would drag the bytes through every
// listing.
//
// Keyed by content hash rather than by model identifier, because that is what
// names it everywhere else and two models with one hash share one blob.
type blobRow struct {
	grove.BaseModel `grove:"table:blackstart_networkmodel_blob"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	ContentHash string `grove:"content_hash,notnull"`
	Body        []byte `grove:"body,notnull"`
	SizeBytes   int    `grove:"size_bytes,notnull"`
}

// toDomain maps a stored row onto the domain type.
func (r modelRow) toDomain() (model.NetworkModel, error) {
	modelID, err := id.Parse(r.ID)
	if err != nil {
		return model.NetworkModel{}, err
	}

	out := model.NetworkModel{
		ID:          modelID,
		ContentHash: r.ContentHash,
		Name:        r.Name,
		SourceKind:  r.SourceKind,
		BusCount:    r.BusCount,
		BranchCount: r.BranchCount,
		BuiltAt:     r.BuiltAt,
	}

	// An absent dataset version is a legitimate state — a model built without
	// a reachable provenance store — so an empty column is Nil rather than a
	// parse failure.
	if r.DatasetVersionID != "" {
		datasetID, err := id.Parse(r.DatasetVersionID)
		if err != nil {
			return model.NetworkModel{}, err
		}

		out.DatasetVersionID = datasetID
	}

	return out, nil
}

// fromDomain maps a domain model onto the persistence shape.
func fromDomain(in model.NetworkModel) modelRow {
	row := modelRow{
		ID:          in.ID.String(),
		ContentHash: in.ContentHash,
		Name:        in.Name,
		SourceKind:  in.SourceKind,
		BusCount:    in.BusCount,
		BranchCount: in.BranchCount,
		BuiltAt:     in.BuiltAt,
	}

	if !in.DatasetVersionID.IsZero() {
		row.DatasetVersionID = in.DatasetVersionID.String()
	}

	return row
}
