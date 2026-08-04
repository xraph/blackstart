package fabriq

import (
	"context"
	"fmt"

	"github.com/xraph/fabriq"

	"github.com/xraph/blackstart/extensions/cascade/run"
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
		return nil, fmt.Errorf("cascade: building the store: %w", err)
	}

	return &Store{base: base}, nil
}

// StoreName identifies the store in health output and migration logs.
func (s *Store) StoreName() string { return "cascade/fabriq" }

// Migrate is a no-op here; schema changes run through the group in
// migrations.go.
func (s *Store) Migrate(context.Context) error { return nil }

// Ping reports whether the source of truth is reachable.
func (s *Store) Ping(ctx context.Context) error { return s.base.Ping(ctx) }

// Close releases nothing: the Fabriq facade is owned by the fabric extension.
func (s *Store) Close() error { return nil }

// Runs returns the run store.
func (s *Store) Runs() run.Store { return (*runs)(s) }

type runs Store

// Create records a run.
//
// Append-only: the command path refuses a second write to the same identifier,
// and that refusal is the rule rather than an accident of the backend. A
// cascade is a statement about what was simulated at one instant, and
// overwriting it would replace a figure someone may already have acted on.
func (s *runs) Create(ctx context.Context, in run.Run) (run.Run, error) {
	if err := in.Validate(); err != nil {
		return run.Run{}, err
	}

	row, err := fromDomain(in)
	if err != nil {
		return run.Run{}, err
	}

	if err := s.base.Create(ctx, RunEntity, in.ID, row); err != nil {
		return run.Run{}, err
	}

	return in, nil
}

// Get retrieves one run.
func (s *runs) Get(ctx context.Context, want id.ID) (run.Run, error) {
	var row runRow

	if err := s.base.Get(ctx, RunEntity, want, &row); err != nil {
		return run.Run{}, err
	}

	return row.toDomain()
}

// defaultListLimit bounds an unbounded request.
//
// The memory store returns everything on a limit of zero, which is what a CLI
// wants and what a table with a few dozen rows can afford. Against a database
// the same call would be a full scan, so this one clamps instead — the two
// stores differ here, and the difference is stated rather than discovered.
const defaultListLimit = 50

// ListByModel returns runs for one network, most recent first.
func (s *runs) ListByModel(
	ctx context.Context, modelHash string, limit int,
) ([]run.Run, error) {
	if modelHash == "" {
		return nil, fmt.Errorf(
			"%w: listing runs needs a model hash; every run names the network it "+
				"simulated", bserr.ErrInvalid)
	}

	if limit <= 0 {
		limit = defaultListLimit
	}

	var rows []runRow

	// Ordered in SQL rather than in Go, because the index is already on
	// (tenant_id, model_hash, started_at DESC). Identifier breaks the tie so
	// two runs recorded in the same instant still order reproducibly, which is
	// the same rule the memory store sorts by.
	if err := s.base.Query(ctx, &rows,
		`SELECT * FROM `+RunTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND model_hash = $1
		 ORDER BY started_at DESC, id DESC
		 LIMIT $2`, modelHash, limit); err != nil {
		return nil, err
	}

	out := make([]run.Run, 0, len(rows))

	for _, row := range rows {
		mapped, err := row.toDomain()
		if err != nil {
			return nil, err
		}

		out = append(out, mapped)
	}

	return out, nil
}
