package fabriq

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xraph/fabriq"

	sid "github.com/xraph/blackstart/extensions/scenario/id"
	"github.com/xraph/blackstart/extensions/scenario/scenario"
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
		return nil, fmt.Errorf("scenario: building the store: %w", err)
	}

	return &Store{base: base}, nil
}

// StoreName identifies the store in health output and migration logs.
func (s *Store) StoreName() string { return "scenario/fabriq" }

// Migrate is a no-op here; schema changes run through the group in
// migrations.go.
func (s *Store) Migrate(context.Context) error { return nil }

// Ping reports whether the source of truth is reachable.
func (s *Store) Ping(ctx context.Context) error { return s.base.Ping(ctx) }

// Close releases nothing: the Fabriq facade is owned by the fabric extension.
func (s *Store) Close() error { return nil }

// Scenarios returns the scenario store.
func (s *Store) Scenarios() scenario.Store { return (*scenarios)(s) }

// Runs returns the run store.
func (s *Store) Runs() scenario.RunStore { return (*runs)(s) }

type scenarios Store

// Create records a scenario, rejecting a duplicate identifier rather than
// overwriting it — the same rule the memory store enforces, backed here by
// the primary key rather than a map lookup.
func (s *scenarios) Create(
	ctx context.Context, in scenario.Scenario,
) (scenario.Scenario, error) {
	if err := in.Manifest.Validate(); err != nil {
		return scenario.Scenario{}, err
	}

	row, err := scenarioFromDomain(in)
	if err != nil {
		return scenario.Scenario{}, err
	}

	if err := s.base.Create(ctx, ScenarioEntity, in.ID, row); err != nil {
		return scenario.Scenario{}, err
	}

	return in, nil
}

// Get retrieves one scenario.
func (s *scenarios) Get(ctx context.Context, scenarioID id.ID) (scenario.Scenario, error) {
	var row scenarioRow

	if err := s.base.Get(ctx, ScenarioEntity, scenarioID, &row); err != nil {
		return scenario.Scenario{}, err
	}

	return row.toDomain()
}

type runs Store

// Create records a run, rejecting a duplicate identifier rather than
// overwriting: runs are append-only, and Create does not decide a run's
// status for the caller — the same contract scenario.RunStore documents.
func (s *runs) Create(ctx context.Context, in scenario.Run) (scenario.Run, error) {
	if err := in.Validate(); err != nil {
		return scenario.Run{}, err
	}

	row, err := runFromDomain(in)
	if err != nil {
		return scenario.Run{}, err
	}

	if err := s.base.Create(ctx, RunEntity, in.ID, row); err != nil {
		return scenario.Run{}, err
	}

	return in, nil
}

// Get retrieves one run.
func (s *runs) Get(ctx context.Context, runID id.ID) (scenario.Run, error) {
	var row runRow

	if err := s.base.Get(ctx, RunEntity, runID, &row); err != nil {
		return scenario.Run{}, err
	}

	return row.toDomain()
}

// ListByScenario returns runs for one scenario, most recent first.
//
// A limit of zero or less returns everything, matching scenario.RunStore's
// own documentation and the memory store's behavior. This store does not
// clamp that to some default: a caller that asked for everything and silently
// got the newest 50 of 60 has no way to tell it happened, and the bound
// belongs at the caller that knows what it wants — Task 7's HTTP surface
// always passes an explicit one.
func (s *runs) ListByScenario(
	ctx context.Context, scenarioID id.ID, limit int,
) ([]scenario.Run, error) {
	var rows []runRow

	// Ordered in SQL rather than in Go, because the index is already on
	// (tenant_id, scenario_id, queued_at DESC). Identifier breaks the tie so
	// two runs queued in the same instant still order reproducibly, the same
	// rule the memory store sorts by.
	query := `SELECT * FROM ` + RunTable + `
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND scenario_id = $1
		 ORDER BY queued_at DESC, id DESC`

	args := []any{scenarioID.String()}

	if limit > 0 {
		query += ` LIMIT $2`
		args = append(args, limit)
	}

	if err := s.base.Query(ctx, &rows, query, args...); err != nil {
		return nil, err
	}

	out := make([]scenario.Run, 0, len(rows))

	for _, row := range rows {
		mapped, err := row.toDomain()
		if err != nil {
			return nil, err
		}

		out = append(out, mapped)
	}

	return out, nil
}

// Claim takes the oldest queued run, marks it running, and returns it.
//
// The memory store's Claim holds one Lock across scan-decide-write, so two
// concurrent callers can never receive the same run. A SELECT followed by a
// separate UPDATE from Go would reopen exactly that race against a database —
// two callers could both read the same queued run before either writes, and
// both would then believe they own it.
//
// This avoids that without needing a hand-rolled lock, by using the
// optimistic-concurrency check fabriqstore.Base.Update already provides:
// ExpectedVersion. Fabriq's command executor reads the aggregate's current
// version with SELECT ... FOR UPDATE and checks it against ExpectedVersion
// inside the same transaction as the write (core/command/executor.go,
// adapters/postgres/tx.go). So when two callers race for one row, the second
// blocks on that row lock until the first commits, then sees the bumped
// version and fails its own check — which fabriqstore.Translate turns into
// bserr.ErrConflict. That is a real database-side atomic hand-off, not a
// read-then-write with a gap in the middle: the version check and the write
// happen under the same row lock, in the same transaction.
//
// A conflict is treated as "someone else got there first" and retried against
// whatever is now the oldest queued run, rather than surfaced to the caller —
// the same way a losing goroutine in the memory store simply does not see the
// run it lost the race for.
func (s *runs) Claim(ctx context.Context) (scenario.Run, error) {
	for {
		var candidates []runRow

		if err := s.base.Query(ctx, &candidates,
			`SELECT * FROM `+RunTable+`
			 WHERE tenant_id = current_setting('app.tenant_id', true)
			   AND status = $1
			 ORDER BY queued_at ASC, id ASC
			 LIMIT 1`, string(scenario.StatusQueued)); err != nil {
			return scenario.Run{}, err
		}

		if len(candidates) == 0 {
			return scenario.Run{}, fmt.Errorf("%w: no run is queued", bserr.ErrNotFound)
		}

		candidate := candidates[0]

		runID, err := sid.ParseRun(candidate.ID)
		if err != nil {
			return scenario.Run{}, fmt.Errorf(
				"scenario: stored run id %q: %w", candidate.ID, err)
		}

		claimed := candidate
		claimed.Status = string(scenario.StatusRunning)
		claimed.StartedAt = time.Now().UTC()

		version := candidate.Version

		if err := s.base.Update(ctx, RunEntity, runID, claimed, &version); err != nil {
			if errors.Is(err, bserr.ErrConflict) {
				continue
			}

			return scenario.Run{}, err
		}

		return claimed.toDomain()
	}
}
