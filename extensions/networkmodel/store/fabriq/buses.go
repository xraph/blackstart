package fabriq

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/xraph/grove"

	"github.com/xraph/blackstart/extensions/networkmodel/model"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// busRow is the persistence shape of one bus of a network model.
//
// The first three fields are Fabriq's structural contract: every entity
// declares id, tenant_id and version, and the registry refuses a model that
// does not.
type busRow struct {
	grove.BaseModel `grove:"table:blackstart_networkmodel_bus"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	ModelID string `grove:"model_id,notnull"`
	Ordinal string `grove:"ordinal,notnull"`

	// VNominalV carries no notnull tag. The DDL says NOT NULL DEFAULT 0, which
	// a zero satisfies, but fabriq's notnull is stricter than the column's and
	// refuses a zero value outright — and a bus stating no voltage is the
	// ordinary case for every IEEE case in the corpus.
	VNominalV float64 `grove:"v_nominal_v"`
}

// busToRow maps a domain bus onto the persistence shape.
func busToRow(b model.Bus) *busRow {
	return &busRow{
		ID:        b.ID.String(),
		ModelID:   b.ModelID.String(),
		Ordinal:   b.Ordinal,
		VNominalV: b.VNominalV,
	}
}

// toDomain maps a stored row onto the domain type.
func (r busRow) toDomain() (model.Bus, error) {
	busID, err := id.Parse(r.ID)
	if err != nil {
		return model.Bus{}, err
	}

	modelID, err := id.Parse(r.ModelID)
	if err != nil {
		return model.Bus{}, err
	}

	return model.Bus{
		ID:        busID,
		ModelID:   modelID,
		Ordinal:   r.Ordinal,
		VNominalV: r.VNominalV,
	}, nil
}

type buses Store

// PutAll writes a model's buses, replacing any it already had.
//
// Read-then-delete-then-create, matching branches.PutAll: Fabriq's only write
// path is per-aggregate commands, so there is no raw multi-row DELETE, and
// inventing a second unaudited write path for this table is the alternative.
func (s *buses) PutAll(ctx context.Context, modelID id.ID, in []model.Bus) error {
	for _, bus := range in {
		if err := bus.Validate(); err != nil {
			return err
		}

		if bus.ModelID != modelID {
			return fmt.Errorf(
				"%w: bus %s says it belongs to model %s, not %s",
				bserr.ErrInvalid, bus.ID, bus.ModelID, modelID)
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

		if err := s.base.Delete(ctx, BusEntity, rowID); err != nil {
			return err
		}
	}

	for _, bus := range in {
		if err := s.base.Create(ctx, BusEntity, bus.ID, busToRow(bus)); err != nil {
			return err
		}
	}

	return nil
}

// AddMissing inserts only the buses whose ordinal the model does not already
// have, matching branches.AddMissing: the existing rows are read first,
// scoped by tenant, and their ordinals filter the incoming set client-side —
// so an ordinal already stored is never handed to Create, which is what
// keeps this from ever attempting the insert migration 0004's unique index
// on (tenant_id, model_id, ordinal) would reject.
func (s *buses) AddMissing(ctx context.Context, modelID id.ID, in []model.Bus) (int, error) {
	for _, bus := range in {
		if err := bus.Validate(); err != nil {
			return 0, err
		}

		if bus.ModelID != modelID {
			return 0, fmt.Errorf(
				"%w: bus %s says it belongs to model %s, not %s",
				bserr.ErrInvalid, bus.ID, bus.ModelID, modelID)
		}
	}

	existing, err := s.rowsForModel(ctx, modelID)
	if err != nil {
		return 0, err
	}

	held := make(map[string]struct{}, len(existing))
	for _, row := range existing {
		held[row.Ordinal] = struct{}{}
	}

	inserted := 0

	for _, bus := range in {
		if _, already := held[bus.Ordinal]; already {
			continue
		}

		if err := s.base.Create(ctx, BusEntity, bus.ID, busToRow(bus)); err != nil {
			return inserted, err
		}

		held[bus.Ordinal] = struct{}{}
		inserted++
	}

	return inserted, nil
}

// ForModel returns a model's buses in ordinal order.
func (s *buses) ForModel(ctx context.Context, modelID id.ID) ([]model.Bus, error) {
	rows, err := s.rowsForModel(ctx, modelID)
	if err != nil {
		return nil, err
	}

	// Numeric rather than lexical, sharing ordinalNumber with the branch store
	// in this package: "bus-10" has the same shape and the same hazard as
	// "branch-10".
	slices.SortFunc(rows, func(a, b busRow) int {
		return cmp.Compare(ordinalNumber(a.Ordinal), ordinalNumber(b.Ordinal))
	})

	out := make([]model.Bus, 0, len(rows))

	for _, row := range rows {
		converted, err := row.toDomain()
		if err != nil {
			return nil, err
		}

		out = append(out, converted)
	}

	return out, nil
}

// ByOrdinal finds one bus by the name a branch calls it.
func (s *buses) ByOrdinal(
	ctx context.Context, modelID id.ID, ordinal string,
) (model.Bus, error) {
	var rows []busRow

	if err := s.base.Query(ctx, &rows,
		`SELECT * FROM `+BusTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND model_id = $1
		   AND ordinal = $2`, modelID.String(), ordinal); err != nil {
		return model.Bus{}, err
	}

	if len(rows) == 0 {
		return model.Bus{}, fmt.Errorf(
			"%w: model %s has no %s", bserr.ErrNotFound, modelID, ordinal)
	}

	return rows[0].toDomain()
}

// rowsForModel reads every stored row for a model, unsorted.
func (s *buses) rowsForModel(ctx context.Context, modelID id.ID) ([]busRow, error) {
	var rows []busRow

	if err := s.base.Query(ctx, &rows,
		`SELECT * FROM `+BusTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND model_id = $1`, modelID.String()); err != nil {
		return nil, err
	}

	return rows, nil
}
