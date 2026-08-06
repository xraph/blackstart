package fabriq

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/xraph/grove"

	"github.com/xraph/blackstart/extensions/networkmodel/model"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// branchRow is the persistence shape of one branch of a network model.
//
// The first three fields are Fabriq's structural contract: every entity
// declares id, tenant_id and version, and the registry refuses a model that
// does not.
type branchRow struct {
	grove.BaseModel `grove:"table:blackstart_networkmodel_branch"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	ModelID string `grove:"model_id,notnull"`
	Ordinal string `grove:"ordinal,notnull"`
	FromBus string `grove:"from_bus,notnull"`
	ToBus   string `grove:"to_bus,notnull"`

	// Alias carries no notnull tag. The DDL says NOT NULL DEFAULT '', which an
	// empty string satisfies, but fabriq's notnull is stricter than the
	// column's and refuses an empty value outright — and most branches have
	// no alias worth recording.
	Alias string `grove:"alias"`
}

// branchToRow maps a domain branch onto the persistence shape.
func branchToRow(b model.Branch) *branchRow {
	return &branchRow{
		ID:      b.ID.String(),
		ModelID: b.ModelID.String(),
		Ordinal: b.Ordinal,
		FromBus: b.FromBus,
		ToBus:   b.ToBus,
		Alias:   b.Alias,
	}
}

// toDomain maps a stored row onto the domain type.
func (r branchRow) toDomain() (model.Branch, error) {
	branchID, err := id.Parse(r.ID)
	if err != nil {
		return model.Branch{}, err
	}

	modelID, err := id.Parse(r.ModelID)
	if err != nil {
		return model.Branch{}, err
	}

	return model.Branch{
		ID:      branchID,
		ModelID: modelID,
		Ordinal: r.Ordinal,
		FromBus: r.FromBus,
		ToBus:   r.ToBus,
		Alias:   r.Alias,
	}, nil
}

type branches Store

// PutAll writes a model's branches, replacing any it already had.
//
// Fabriq's only write path is per-aggregate commands — there is no raw
// multi-row DELETE — so the replacement reads the model's existing rows and
// deletes each of them before creating the incoming set. Read-then-write
// rather than an upsert, matching how the hazard extension's assignment store
// replaces a binding: the alternative is inventing a second, unaudited write
// path just for this table.
func (s *branches) PutAll(ctx context.Context, modelID id.ID, in []model.Branch) error {
	for _, branch := range in {
		if err := branch.Validate(); err != nil {
			return err
		}

		if branch.ModelID != modelID {
			return fmt.Errorf(
				"%w: branch %s says it belongs to model %s, not %s",
				bserr.ErrInvalid, branch.ID, branch.ModelID, modelID)
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

		if err := s.base.Delete(ctx, BranchEntity, rowID); err != nil {
			return err
		}
	}

	for _, branch := range in {
		if err := s.base.Create(ctx, BranchEntity, branch.ID, branchToRow(branch)); err != nil {
			return err
		}
	}

	return nil
}

// ForModel returns a model's branches in ordinal order.
func (s *branches) ForModel(ctx context.Context, modelID id.ID) ([]model.Branch, error) {
	rows, err := s.rowsForModel(ctx, modelID)
	if err != nil {
		return nil, err
	}

	// Numeric rather than lexical, because sorting these as strings puts
	// branch-10 between branch-1 and branch-2 — not an ordering anybody
	// reading a network expects.
	slices.SortFunc(rows, func(a, b branchRow) int {
		return cmp.Compare(ordinalNumber(a.Ordinal), ordinalNumber(b.Ordinal))
	})

	out := make([]model.Branch, 0, len(rows))

	for _, row := range rows {
		converted, err := row.toDomain()
		if err != nil {
			return nil, err
		}

		out = append(out, converted)
	}

	return out, nil
}

// Get finds one branch by its identifier.
func (s *branches) Get(ctx context.Context, branchID id.ID) (model.Branch, error) {
	var rows []branchRow

	if err := s.base.Query(ctx, &rows,
		`SELECT * FROM `+BranchTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND id = $1`, branchID.String()); err != nil {
		return model.Branch{}, err
	}

	if len(rows) == 0 {
		return model.Branch{}, fmt.Errorf(
			"%w: no branch %s", bserr.ErrNotFound, branchID)
	}

	return rows[0].toDomain()
}

// ByOrdinal finds one branch by the name the solver uses for it.
func (s *branches) ByOrdinal(
	ctx context.Context, modelID id.ID, ordinal string,
) (model.Branch, error) {
	var rows []branchRow

	if err := s.base.Query(ctx, &rows,
		`SELECT * FROM `+BranchTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND model_id = $1
		   AND ordinal = $2`, modelID.String(), ordinal); err != nil {
		return model.Branch{}, err
	}

	if len(rows) == 0 {
		return model.Branch{}, fmt.Errorf(
			"%w: model %s has no %s", bserr.ErrNotFound, modelID, ordinal)
	}

	return rows[0].toDomain()
}

// rowsForModel reads every stored row for a model, unsorted.
func (s *branches) rowsForModel(ctx context.Context, modelID id.ID) ([]branchRow, error) {
	var rows []branchRow

	if err := s.base.Query(ctx, &rows,
		`SELECT * FROM `+BranchTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND model_id = $1`, modelID.String()); err != nil {
		return nil, err
	}

	return rows, nil
}

// ordinalNumber reads the index out of "branch-10".
//
// An ordinal that does not parse sorts last rather than failing: it is a
// display order, not a correctness property.
func ordinalNumber(ordinal string) int {
	_, digits, found := strings.Cut(ordinal, "-")
	if !found {
		return math.MaxInt
	}

	n, err := strconv.Atoi(digits)
	if err != nil {
		return math.MaxInt
	}

	return n
}
