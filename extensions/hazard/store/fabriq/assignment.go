package fabriq

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/grove"

	"github.com/xraph/blackstart/extensions/hazard/conductor"
	hid "github.com/xraph/blackstart/extensions/hazard/id"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	pkgstore "github.com/xraph/blackstart/pkg/store"
	"github.com/xraph/blackstart/pkg/store/fabriqstore"
)

// assignmentModel is the persistence shape of one branch-to-conductor binding.
type assignmentModel struct {
	grove.BaseModel `grove:"table:blackstart_hazard_assignment"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	BranchID      string `grove:"branch_id,notnull"`
	ConductorCode string `grove:"conductor_code,notnull"`
	Basis         string `grove:"basis,notnull"`

	// Note carries no notnull tag. Most assignments have none, and fabriq's
	// notnull is stricter than the column's: the DDL says NOT NULL DEFAULT '',
	// which an empty string satisfies, but fabriq refuses an empty value for a
	// notnull field outright.
	Note string `grove:"note"`
}

func assignmentToModel(a conductor.Assignment) *assignmentModel {
	return &assignmentModel{
		ID:            a.ID.String(),
		BranchID:      a.BranchID.String(),
		ConductorCode: a.ConductorCode,
		Basis:         string(a.Basis),
		Note:          a.Note,
	}
}

func assignmentFromModel(m assignmentModel) (conductor.Assignment, error) {
	assignmentID, err := hid.ParseAssignment(m.ID)
	if err != nil {
		return conductor.Assignment{}, fmt.Errorf(
			"hazard: stored assignment id %q: %w", m.ID, err)
	}

	branchID, err := id.Parse(m.BranchID)
	if err != nil {
		return conductor.Assignment{}, fmt.Errorf(
			"hazard: stored branch id %q: %w", m.BranchID, err)
	}

	return conductor.Assignment{
		ID:            assignmentID,
		BranchID:      branchID,
		ConductorCode: m.ConductorCode,
		Basis:         conductor.Basis(m.Basis),
		Note:          m.Note,
	}, nil
}

type assignmentStore struct {
	base *fabriqstore.Base
}

// Put writes an assignment, replacing whatever the branch already had.
//
// Read-then-write rather than an upsert, because the command path has no
// upsert and because the replacement is by branch rather than by identifier: a
// survey produces a new assignment for a branch that already has an assumed
// one, and the old row has to go or the unique index rejects the new one.
func (s *assignmentStore) Put(ctx context.Context, in conductor.Assignment) error {
	if err := in.Validate(); err != nil {
		return err
	}

	existing, err := s.ForBranch(ctx, in.BranchID)

	switch {
	case err == nil && existing.ID == in.ID:
		return s.base.Update(ctx, AssignmentEntity, in.ID, assignmentToModel(in), nil)
	case err == nil:
		if err := s.base.Delete(ctx, AssignmentEntity, existing.ID); err != nil {
			return err
		}
	case !errors.Is(err, bserr.ErrNotFound):
		return err
	}

	return s.base.Create(ctx, AssignmentEntity, in.ID, assignmentToModel(in))
}

func (s *assignmentStore) Get(
	ctx context.Context, want id.ID,
) (conductor.Assignment, error) {
	var m assignmentModel

	if err := s.base.Get(ctx, AssignmentEntity, want, &m); err != nil {
		return conductor.Assignment{}, err
	}

	return assignmentFromModel(m)
}

func (s *assignmentStore) ForBranch(
	ctx context.Context, branch id.ID,
) (conductor.Assignment, error) {
	var rows []assignmentModel

	if err := s.base.Query(ctx, &rows,
		`SELECT * FROM `+AssignmentTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND branch_id = $1`, branch.String()); err != nil {
		return conductor.Assignment{}, err
	}

	if len(rows) == 0 {
		return conductor.Assignment{}, fmt.Errorf(
			"%w: no conductor assigned to branch %s", bserr.ErrNotFound, branch)
	}

	return assignmentFromModel(rows[0])
}

func (s *assignmentStore) List(
	ctx context.Context, p pkgstore.Page,
) ([]conductor.Assignment, error) {
	p = p.Normalize()

	var rows []assignmentModel

	after := ""
	if !p.After.IsZero() {
		after = p.After.String()
	}

	// The empty string sorts before every identifier, so one query serves both
	// the first page and every page after it.
	if err := s.base.Query(ctx, &rows,
		`SELECT * FROM `+AssignmentTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND id > $1
		 ORDER BY id ASC
		 LIMIT $2`, after, p.Limit); err != nil {
		return nil, err
	}

	out := make([]conductor.Assignment, 0, len(rows))

	for _, row := range rows {
		got, err := assignmentFromModel(row)
		if err != nil {
			return nil, err
		}

		out = append(out, got)
	}

	return out, nil
}

func (s *assignmentStore) Delete(ctx context.Context, want id.ID) error {
	return s.base.Delete(ctx, AssignmentEntity, want)
}
