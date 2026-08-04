package fabriq

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/grove"

	hid "github.com/xraph/blackstart/extensions/hazard/id"
	"github.com/xraph/blackstart/extensions/hazard/store"
	"github.com/xraph/blackstart/pkg/id"
	pkgstore "github.com/xraph/blackstart/pkg/store"
	"github.com/xraph/blackstart/pkg/store/fabriqstore"
)

// computationModel is the persistence shape of one pass of the models.
type computationModel struct {
	grove.BaseModel `grove:"table:blackstart_hazard_computation"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	CycleID string `grove:"cycle_id,notnull"`

	// RunID carries no notnull tag. A pass recorded against a deployment
	// without a provenance recorder has no run, and refusing to store the
	// computation would lose the coverage counts along with the missing link.
	RunID string `grove:"run_id"`

	ComputedAt time.Time `grove:"computed_at,notnull"`

	// Counts carry no notnull tag either: fabriq refuses a zero value for one,
	// and a pass that rated nothing is exactly the pass worth recording.
	Spans   int `grove:"spans"`
	Unrated int `grove:"unrated"`
	// The column is value_count rather than values: VALUES is a reserved word
	// in Postgres, and an unquoted one in a generated INSERT is a syntax error
	// rather than a subtle bug.
	Values int `grove:"value_count"`
}

func computationToModel(c store.Computation) *computationModel {
	m := &computationModel{
		ID:         c.ID.String(),
		CycleID:    c.CycleID.String(),
		ComputedAt: c.ComputedAt.UTC(),
		Spans:      c.Spans,
		Unrated:    c.Unrated,
		Values:     c.Values,
	}

	if !c.RunID.IsZero() {
		m.RunID = c.RunID.String()
	}

	return m
}

func computationFromModel(m computationModel) (store.Computation, error) {
	computationID, err := hid.ParseComputation(m.ID)
	if err != nil {
		return store.Computation{}, fmt.Errorf(
			"hazard: stored computation id %q: %w", m.ID, err)
	}

	cycleID, err := id.Parse(m.CycleID)
	if err != nil {
		return store.Computation{}, fmt.Errorf(
			"hazard: stored cycle id %q: %w", m.CycleID, err)
	}

	out := store.Computation{
		ID:         computationID,
		CycleID:    cycleID,
		ComputedAt: m.ComputedAt.UTC(),
		Spans:      m.Spans,
		Unrated:    m.Unrated,
		Values:     m.Values,
	}

	if m.RunID != "" {
		runID, err := id.Parse(m.RunID)
		if err != nil {
			return store.Computation{}, fmt.Errorf(
				"hazard: stored run id %q: %w", m.RunID, err)
		}

		out.RunID = runID
	}

	return out, nil
}

type computationStore struct {
	base *fabriqstore.Base
}

func (s *computationStore) Create(ctx context.Context, in store.Computation) error {
	if err := in.Validate(); err != nil {
		return err
	}

	return s.base.Create(ctx, ComputationEntity, in.ID, computationToModel(in))
}

func (s *computationStore) Get(
	ctx context.Context, want id.ID,
) (store.Computation, error) {
	var m computationModel

	if err := s.base.Get(ctx, ComputationEntity, want, &m); err != nil {
		return store.Computation{}, err
	}

	return computationFromModel(m)
}

func (s *computationStore) ForCycle(
	ctx context.Context, cycle id.ID,
) ([]store.Computation, error) {
	var rows []computationModel

	// Ordered in SQL rather than in Go, because the index is already on
	// (tenant_id, cycle_id, computed_at DESC). Identifier breaks the tie: two
	// passes over one cycle in the same second is a recomputation triggered
	// right after the first, and an unstable order would answer differently
	// each call.
	if err := s.base.Query(ctx, &rows,
		`SELECT * FROM `+ComputationTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND cycle_id = $1
		 ORDER BY computed_at DESC, id DESC`, cycle.String()); err != nil {
		return nil, err
	}

	return computationsFromModels(rows)
}

func (s *computationStore) List(
	ctx context.Context, p pkgstore.Page,
) ([]store.Computation, error) {
	p = p.Normalize()

	var rows []computationModel

	// Newest first, so the cursor walks downward: the page after an identifier
	// is the rows below it, not above. An empty cursor matches everything
	// rather than needing a second query for the first page.
	after := ""
	if !p.After.IsZero() {
		after = p.After.String()
	}

	if err := s.base.Query(ctx, &rows,
		`SELECT * FROM `+ComputationTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND ($1 = '' OR id < $1)
		 ORDER BY computed_at DESC, id DESC
		 LIMIT $2`, after, p.Limit); err != nil {
		return nil, err
	}

	return computationsFromModels(rows)
}

func computationsFromModels(rows []computationModel) ([]store.Computation, error) {
	out := make([]store.Computation, 0, len(rows))

	for _, row := range rows {
		got, err := computationFromModel(row)
		if err != nil {
			return nil, err
		}

		out = append(out, got)
	}

	return out, nil
}
