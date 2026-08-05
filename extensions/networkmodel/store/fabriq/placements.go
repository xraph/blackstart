package fabriq

import (
	"context"
	"fmt"

	"github.com/xraph/grove"

	"github.com/xraph/blackstart/extensions/networkmodel/model"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// placementRow is the persistence shape of where one bus sits on the earth.
//
// The first three fields are Fabriq's structural contract: every entity
// declares id, tenant_id and version, and the registry refuses a model that
// does not.
type placementRow struct {
	grove.BaseModel `grove:"table:blackstart_networkmodel_placement"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	ModelID   string  `grove:"model_id,notnull"`
	Bus       string  `grove:"bus,notnull"`
	Lon       float64 `grove:"lon,notnull"`
	Lat       float64 `grove:"lat,notnull"`
	Elevation float64 `grove:"elevation,notnull"`
}

// placementToRow maps a domain placement onto the persistence shape.
func placementToRow(p model.Placement) *placementRow {
	return &placementRow{
		ID:        p.ID.String(),
		ModelID:   p.ModelID.String(),
		Bus:       p.Bus,
		Lon:       p.Lon,
		Lat:       p.Lat,
		Elevation: p.Elevation,
	}
}

// toDomain maps a stored row onto the domain type.
func (r placementRow) toDomain() (model.Placement, error) {
	placementID, err := id.Parse(r.ID)
	if err != nil {
		return model.Placement{}, err
	}

	modelID, err := id.Parse(r.ModelID)
	if err != nil {
		return model.Placement{}, err
	}

	return model.Placement{
		ID:        placementID,
		ModelID:   modelID,
		Bus:       r.Bus,
		Lon:       r.Lon,
		Lat:       r.Lat,
		Elevation: r.Elevation,
	}, nil
}

type placements Store

// PutAll writes a model's placements, replacing any it already had.
//
// Fabriq's only write path is per-aggregate commands — there is no raw
// multi-row DELETE — so the replacement reads the model's existing rows and
// deletes each of them before creating the incoming set, the same way
// branches.PutAll does.
func (s *placements) PutAll(ctx context.Context, modelID id.ID, in []model.Placement) error {
	for _, placement := range in {
		if err := placement.Validate(); err != nil {
			return err
		}

		if placement.ModelID != modelID {
			return fmt.Errorf(
				"%w: placement %s says it belongs to model %s, not %s",
				bserr.ErrInvalid, placement.ID, placement.ModelID, modelID)
		}
	}

	existing, err := s.rowsForModel(ctx, modelID)
	if err != nil {
		return err
	}

	for _, row := range existing {
		rowID, err := id.Parse(row.ID)
		if err != nil {
			return err
		}

		if err := s.base.Delete(ctx, PlacementEntity, rowID); err != nil {
			return err
		}
	}

	for _, placement := range in {
		if err := s.base.Create(ctx, PlacementEntity, placement.ID, placementToRow(placement)); err != nil {
			return err
		}
	}

	return nil
}

// ForModel returns every placement a model has.
func (s *placements) ForModel(ctx context.Context, modelID id.ID) ([]model.Placement, error) {
	rows, err := s.rowsForModel(ctx, modelID)
	if err != nil {
		return nil, err
	}

	out := make([]model.Placement, 0, len(rows))

	for _, row := range rows {
		converted, err := row.toDomain()
		if err != nil {
			return nil, err
		}

		out = append(out, converted)
	}

	return out, nil
}

// rowsForModel reads every stored row for a model.
func (s *placements) rowsForModel(ctx context.Context, modelID id.ID) ([]placementRow, error) {
	var rows []placementRow

	if err := s.base.Query(ctx, &rows,
		`SELECT * FROM `+PlacementTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND model_id = $1`, modelID.String()); err != nil {
		return nil, err
	}

	return rows, nil
}
