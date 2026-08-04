package fabriq

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/blackstart/extensions/provenance/record"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/store/fabriqstore"
)

// records implements record.Store.
//
// There is no Update and no Delete, and there will not be. A provenance record
// states what happened; correcting one would mean claiming a different thing
// happened, which is falsification rather than correction. The database agrees
// — every table carries a trigger that refuses both — so this is a property of
// the data rather than an omission in this file.
type records struct {
	base *fabriqstore.Base
}

// CreateDataset records one retrieval.
func (r *records) CreateDataset(
	ctx context.Context, in record.DatasetVersion,
) (record.DatasetVersion, error) {
	if err := in.Validate(); err != nil {
		return record.DatasetVersion{}, err
	}

	model, err := datasetToModel(in)
	if err != nil {
		return record.DatasetVersion{}, err
	}

	if err := r.base.Create(ctx, DatasetEntity, in.ID, model); err != nil {
		// Fabriq reports a unique-index violation as a conflict, which is what
		// the index on (tenant_id, content_hash) produces when these bytes
		// have already been retrieved. The index is the authority rather than
		// a read-then-write check, which two concurrent ingests would both
		// pass — so the collision is absorbed here and the record that is
		// already stored is returned.
		if errors.Is(err, bserr.ErrConflict) {
			if existing, findErr := r.FindDatasetByHash(ctx, in.ContentHash); findErr == nil {
				return existing, nil
			}
		}

		return record.DatasetVersion{}, err
	}

	return in, nil
}

// GetDataset retrieves one retrieval.
func (r *records) GetDataset(
	ctx context.Context, want id.ID,
) (record.DatasetVersion, error) {
	var m datasetModel

	if err := r.base.Get(ctx, DatasetEntity, want, &m); err != nil {
		return record.DatasetVersion{}, fmt.Errorf(
			"provenance: dataset version %s: %w", want, err)
	}

	return datasetFromModel(m)
}

// FindDatasetByHash answers "have we already retrieved these exact bytes".
//
// The unique index on (tenant_id, content_hash) is what makes this a lookup
// rather than a scan, and it is also what makes the answer unambiguous: there
// cannot be two.
func (r *records) FindDatasetByHash(
	ctx context.Context, contentHash string,
) (record.DatasetVersion, error) {
	var rows []datasetModel

	// Base.Query, not Base.QueryAsOf: a provenance record has no valid-time
	// interval to filter on, and the temporal predicate would compose against
	// columns that do not exist.
	if err := r.base.Query(ctx, &rows,
		`SELECT * FROM `+DatasetTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND content_hash = $1
		 LIMIT 1`, contentHash); err != nil {
		return record.DatasetVersion{}, err
	}

	if len(rows) == 0 {
		return record.DatasetVersion{}, fmt.Errorf(
			"%w: no dataset version with content hash %s", bserr.ErrNotFound, contentHash)
	}

	return datasetFromModel(rows[0])
}

// CreateModel records one producer version.
func (r *records) CreateModel(
	ctx context.Context, in record.ModelVersion,
) (record.ModelVersion, error) {
	if err := in.Validate(); err != nil {
		return record.ModelVersion{}, err
	}

	model, err := modelToModel(in)
	if err != nil {
		return record.ModelVersion{}, err
	}

	if err := r.base.Create(ctx, ModelEntity, in.ID, model); err != nil {
		return record.ModelVersion{}, err
	}

	return in, nil
}

// GetModel retrieves one producer version.
func (r *records) GetModel(ctx context.Context, want id.ID) (record.ModelVersion, error) {
	var m modelModel

	if err := r.base.Get(ctx, ModelEntity, want, &m); err != nil {
		return record.ModelVersion{}, fmt.Errorf(
			"provenance: model version %s: %w", want, err)
	}

	return modelFromModel(m)
}

// CreateRun records one execution.
func (r *records) CreateRun(ctx context.Context, in record.Run) (record.Run, error) {
	if err := in.Validate(); err != nil {
		return record.Run{}, err
	}

	if err := r.base.Create(ctx, RunEntity, in.ID, runToModel(in)); err != nil {
		// A foreign key violation and a duplicate identifier arrive with the
		// same Fabriq code, so the two are told apart by asking which is true.
		// The distinction matters: a missing producer is the caller's error
		// and a duplicate identifier is a collision, and the in-memory store
		// already reports them as ErrInvalid and ErrConflict respectively.
		if errors.Is(err, bserr.ErrConflict) {
			if _, missing := r.GetModel(ctx, in.ModelVersionID); missing != nil {
				return record.Run{}, fmt.Errorf(
					"%w: run %s names model version %s, which was never recorded",
					bserr.ErrInvalid, in.ID, in.ModelVersionID)
			}
		}

		return record.Run{}, err
	}

	return in, nil
}

// GetRun retrieves one execution.
func (r *records) GetRun(ctx context.Context, want id.ID) (record.Run, error) {
	var m runModel

	if err := r.base.Get(ctx, RunEntity, want, &m); err != nil {
		return record.Run{}, fmt.Errorf("provenance: run %s: %w", want, err)
	}

	return runFromModel(m)
}

// CreateLineage records that a run used a dataset.
func (r *records) CreateLineage(
	ctx context.Context, in record.Lineage,
) (record.Lineage, error) {
	if err := in.Validate(); err != nil {
		return record.Lineage{}, err
	}

	if err := r.base.Create(ctx, LineageEntity, in.ID, lineageToModel(in)); err != nil {
		// Both ends have to exist. An edge to a record that was never written
		// is a dangling reference in the one structure whose whole value is
		// that it can be followed.
		if errors.Is(err, bserr.ErrConflict) {
			if _, missing := r.GetRun(ctx, in.RunID); missing != nil {
				return record.Lineage{}, fmt.Errorf(
					"%w: lineage names run %s, which was never recorded",
					bserr.ErrInvalid, in.RunID)
			}

			if _, missing := r.GetDataset(ctx, in.DatasetVersionID); missing != nil {
				return record.Lineage{}, fmt.Errorf(
					"%w: lineage names dataset version %s, which was never recorded",
					bserr.ErrInvalid, in.DatasetVersionID)
			}
		}

		return record.Lineage{}, err
	}

	return in, nil
}

// LineageOf returns every input a run used, in the order recorded.
//
// Ordered by identifier, which sorts chronologically — the identifiers are
// time-ordered, so this reproduces insertion order without a column that says
// so and without depending on the heap order the database happens to return.
func (r *records) LineageOf(ctx context.Context, runID id.ID) ([]record.Lineage, error) {
	var rows []lineageModel

	if err := r.base.Query(ctx, &rows,
		`SELECT * FROM `+LineageTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND run_id = $1
		 ORDER BY id ASC`, runID.String()); err != nil {
		return nil, err
	}

	out := make([]record.Lineage, 0, len(rows))

	for _, row := range rows {
		edge, err := lineageFromModel(row)
		if err != nil {
			return nil, err
		}

		out = append(out, edge)
	}

	return out, nil
}
