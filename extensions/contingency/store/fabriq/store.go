package fabriq

import (
	"context"
	"fmt"

	"github.com/xraph/fabriq"

	"github.com/xraph/blackstart/extensions/contingency/sweep"
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
		return nil, fmt.Errorf("contingency: building the store: %w", err)
	}

	return &Store{base: base}, nil
}

// StoreName identifies the store in health output and migration logs.
func (s *Store) StoreName() string { return "contingency/fabriq" }

// Migrate is a no-op here; schema changes run through the group in
// migrations.go.
func (s *Store) Migrate(context.Context) error { return nil }

// Ping reports whether the source of truth is reachable.
func (s *Store) Ping(ctx context.Context) error { return s.base.Ping(ctx) }

// Close releases nothing: the Fabriq facade is owned by the fabric extension.
func (s *Store) Close() error { return nil }

// Runs returns the run store.
func (s *Store) Runs() sweep.Store { return (*runs)(s) }

type runs Store

// Create records a run.
func (s *runs) Create(ctx context.Context, in sweep.Run) (sweep.Run, error) {
	if err := in.Validate(); err != nil {
		return sweep.Run{}, err
	}

	row, err := fromDomain(in)
	if err != nil {
		return sweep.Run{}, err
	}

	if err := s.base.Create(ctx, RunEntity, in.ID, row); err != nil {
		return sweep.Run{}, err
	}

	return in, nil
}

// Get retrieves one run.
func (s *runs) Get(ctx context.Context, want id.ID) (sweep.Run, error) {
	var row runRow

	if err := s.base.Get(ctx, RunEntity, want, &row); err != nil {
		return sweep.Run{}, err
	}

	return row.toDomain()
}

// ListByModel returns runs for one network, most recent first.
//
// The hash may be in either space — sweep.Store.ListByModel says why. The SQL
// below is that predicate expressed for the database; sweep.Run.NamesModel is
// the same rule in Go, and the two are checked against each other by the
// integration tests rather than trusted to stay aligned.
func (s *runs) ListByModel(
	ctx context.Context, modelHash string, limit int,
) ([]sweep.Run, error) {
	if modelHash == "" {
		return nil, fmt.Errorf(
			"%w: listing runs needs a model hash; every run names the network it "+
				"screened", bserr.ErrInvalid)
	}

	if limit <= 0 {
		limit = 50
	}

	var rows []runRow

	// An OR over two indexed columns rather than two queries stitched
	// together: Postgres can bitmap-or the model and canonical indexes, and a
	// union would have to re-sort and re-limit the combined result in Go,
	// which is where the two stores' orderings would start to differ. NULL
	// canonical_hash simply never matches, which is what "unknown" should do.
	if err := s.base.Query(ctx, &rows,
		`SELECT * FROM `+RunTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND (model_hash = $1 OR canonical_hash = $1)
		 ORDER BY started_at DESC, id DESC
		 LIMIT $2`, modelHash, limit); err != nil {
		return nil, err
	}

	out := make([]sweep.Run, 0, len(rows))

	for _, row := range rows {
		mapped, err := row.toDomain()
		if err != nil {
			return nil, err
		}

		out = append(out, mapped)
	}

	return out, nil
}
