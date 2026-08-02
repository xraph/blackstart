package fabriq

import (
	"context"
	"fmt"

	"github.com/xraph/fabriq"

	"github.com/xraph/blackstart/extensions/powerflow/solution"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/store/fabriqstore"
)

// Store is the Fabriq-backed composite.
type Store struct {
	base *fabriqstore.Base
}

// New builds the store over a Fabriq facade.
func New(fab *fabriq.Fabriq, tenant string) (*Store, error) {
	base, err := fabriqstore.New(fab, tenant)
	if err != nil {
		return nil, fmt.Errorf("powerflow: building the store: %w", err)
	}

	return &Store{base: base}, nil
}

// StoreName identifies the store in health output and migration logs.
func (s *Store) StoreName() string { return "powerflow/fabriq" }

// Migrate is a no-op here; schema changes run through the group in
// migrations.go.
func (s *Store) Migrate(context.Context) error { return nil }

// Ping reports whether the source of truth is reachable.
func (s *Store) Ping(ctx context.Context) error { return s.base.Ping(ctx) }

// Close releases nothing: the Fabriq facade is owned by the fabric extension.
func (s *Store) Close() error { return nil }

// Solutions returns the solution store.
func (s *Store) Solutions() solution.Store { return (*solutions)(s) }

type solutions Store

// Create records a solution.
func (s *solutions) Create(
	ctx context.Context, in solution.Solution,
) (solution.Solution, error) {
	if err := in.Validate(); err != nil {
		return solution.Solution{}, err
	}

	row, err := fromDomain(in)
	if err != nil {
		return solution.Solution{}, err
	}

	if err := s.base.Create(ctx, SolutionEntity, in.ID, row); err != nil {
		return solution.Solution{}, err
	}

	return in, nil
}

// Get retrieves one solution.
func (s *solutions) Get(ctx context.Context, want id.ID) (solution.Solution, error) {
	var row solutionRow

	if err := s.base.Get(ctx, SolutionEntity, want, &row); err != nil {
		return solution.Solution{}, err
	}

	return row.toDomain()
}

// ListByModel returns solutions for one network, most recent first.
func (s *solutions) ListByModel(
	ctx context.Context, modelHash string, limit int,
) ([]solution.Solution, error) {
	if modelHash == "" {
		return nil, fmt.Errorf(
			"%w: listing solutions needs a model hash; every solution names the "+
				"network it was computed against", bserr.ErrInvalid)
	}

	if limit <= 0 {
		limit = 50
	}

	var rows []solutionRow

	if err := s.base.Query(ctx, &rows,
		`SELECT * FROM `+SolutionTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND model_hash = $1
		 ORDER BY solved_at DESC, id DESC
		 LIMIT $2`, modelHash, limit); err != nil {
		return nil, err
	}

	out := make([]solution.Solution, 0, len(rows))

	for _, row := range rows {
		mapped, err := row.toDomain()
		if err != nil {
			return nil, err
		}

		out = append(out, mapped)
	}

	return out, nil
}
