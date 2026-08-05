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

// Update replaces a stored scenario.
//
// The read before the write is for the version, not for existence: fabriq's
// executor already refuses to update an aggregate it holds no version for,
// which is what makes a scenario nobody defined come back not-found. What the
// read buys is the expected version, so that two people editing one scenario
// produce a conflict rather than one of the two edits vanishing — the same
// optimistic check Claim relies on, used here for the ordinary lost-update
// case rather than for a hand-off.
func (s *scenarios) Update(
	ctx context.Context, in scenario.Scenario,
) (scenario.Scenario, error) {
	if err := in.Manifest.Validate(); err != nil {
		return scenario.Scenario{}, err
	}

	var current scenarioRow
	if err := s.base.Get(ctx, ScenarioEntity, in.ID, &current); err != nil {
		return scenario.Scenario{}, err
	}

	row, err := scenarioFromDomain(in)
	if err != nil {
		return scenario.Scenario{}, err
	}

	// The version read a moment ago, so a concurrent edit of the same
	// scenario fails rather than silently overwriting the other one.
	version := current.Version

	if err := s.base.Update(ctx, ScenarioEntity, in.ID, row, &version); err != nil {
		return scenario.Scenario{}, err
	}

	return in, nil
}

// List returns scenarios, most recently created first.
//
// A limit of zero or less returns everything, matching scenario.Store's own
// documentation and the memory store's behavior. No clamp to some default:
// a caller that asked for everything and silently got the newest fifty of
// sixty has no way to tell it happened.
func (s *scenarios) List(ctx context.Context, limit int) ([]scenario.Scenario, error) {
	var rows []scenarioRow

	// Identifier breaks the tie so two scenarios defined in the same instant
	// still order reproducibly, the same rule the memory store sorts by.
	query := `SELECT * FROM ` + ScenarioTable + `
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		 ORDER BY created_at DESC, id DESC`

	var args []any

	if limit > 0 {
		query += ` LIMIT $1`
		args = append(args, limit)
	}

	if err := s.base.Query(ctx, &rows, query, args...); err != nil {
		return nil, err
	}

	out := make([]scenario.Scenario, 0, len(rows))

	for _, row := range rows {
		mapped, err := row.toDomain()
		if err != nil {
			return nil, err
		}

		out = append(out, mapped)
	}

	return out, nil
}

type runs Store

// Create records a run, rejecting a duplicate identifier rather than
// overwriting: runs are append-only, and Create does not decide a run's
// status for the caller — the same contract scenario.RunStore documents.
func (s *runs) Create(ctx context.Context, in scenario.ScenarioRun) (scenario.ScenarioRun, error) {
	if err := in.Validate(); err != nil {
		return scenario.ScenarioRun{}, err
	}

	row, err := runFromDomain(in)
	if err != nil {
		return scenario.ScenarioRun{}, err
	}

	if err := s.base.Create(ctx, RunEntity, in.ID, row); err != nil {
		return scenario.ScenarioRun{}, err
	}

	return in, nil
}

// Get retrieves one run.
func (s *runs) Get(ctx context.Context, runID id.ID) (scenario.ScenarioRun, error) {
	var row runRow

	if err := s.base.Get(ctx, RunEntity, runID, &row); err != nil {
		return scenario.ScenarioRun{}, err
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
) ([]scenario.ScenarioRun, error) {
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

	return runsToDomain(rows)
}

// List returns runs across every scenario, most recent first.
//
// `queued_at DESC, id DESC`, character for character what ListByScenario
// above orders by and what the memory store's newestFirst sorts to. The two
// stores have already disagreed once about what "oldest" meant — Claim scanned
// insertion order in one and queued_at in the other — so the ordering clause
// here is not an independent choice, it is the other half of a pair.
//
// The index behind it is RunTable_recent_idx rather than
// RunTable_scenario_idx: the scenario index leads on scenario_id, so a scan
// that names no scenario cannot walk it in order and Postgres falls back to
// sorting the tenant's whole run table to answer a request for the newest
// hundred rows.
//
// A limit of zero or less returns everything, matching scenario.RunStore's
// own documentation and the memory store's behavior. No clamp here for the
// reason ListByScenario has none: the bound belongs at the caller, and the
// HTTP surface always passes an explicit one.
func (s *runs) List(ctx context.Context, limit int) ([]scenario.ScenarioRun, error) {
	var rows []runRow

	query := `SELECT * FROM ` + RunTable + `
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		 ORDER BY queued_at DESC, id DESC`

	var args []any

	if limit > 0 {
		query += ` LIMIT $1`
		args = append(args, limit)
	}

	if err := s.base.Query(ctx, &rows, query, args...); err != nil {
		return nil, err
	}

	return runsToDomain(rows)
}

// runsToDomain maps a queried page of rows, naming the row that failed by
// stopping at it rather than returning a partial list a caller would read as
// the whole answer.
func runsToDomain(rows []runRow) ([]scenario.ScenarioRun, error) {
	out := make([]scenario.ScenarioRun, 0, len(rows))

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
func (s *runs) Claim(ctx context.Context) (scenario.ScenarioRun, error) {
	for {
		var candidates []runRow

		if err := s.base.Query(ctx, &candidates,
			`SELECT * FROM `+RunTable+`
			 WHERE tenant_id = current_setting('app.tenant_id', true)
			   AND status = $1
			 ORDER BY queued_at ASC, id ASC
			 LIMIT 1`, string(scenario.StatusQueued)); err != nil {
			return scenario.ScenarioRun{}, err
		}

		if len(candidates) == 0 {
			return scenario.ScenarioRun{}, fmt.Errorf("%w: no run is queued", bserr.ErrNotFound)
		}

		candidate := candidates[0]

		runID, err := sid.ParseRun(candidate.ID)
		if err != nil {
			return scenario.ScenarioRun{}, fmt.Errorf(
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

			return scenario.ScenarioRun{}, err
		}

		return claimed.toDomain()
	}
}

// Finish records a run's terminal state.
//
// The status is checked against the row that was just read and the write goes
// out with that row's version, so the check and the write are one atomic step
// for the reason Claim's are: two workers that both believed they owned a run
// would otherwise both write their own answer over it, and the second would
// win silently. Here the version check turns the loser's write into a
// conflict, which is what the memory store's single Lock achieves the same
// way.
//
// A conflict is returned rather than retried, unlike Claim's. Claim retrying
// means "take whatever is queued now", which is still the job. Finish
// retrying would mean overwriting whatever the other worker recorded with
// this one's answer, which is the opposite of the job.
func (s *runs) Finish(ctx context.Context, in scenario.ScenarioRun) (scenario.ScenarioRun, error) {
	if err := in.Finishable(); err != nil {
		return scenario.ScenarioRun{}, err
	}

	var current runRow
	if err := s.base.Get(ctx, RunEntity, in.ID, &current); err != nil {
		return scenario.ScenarioRun{}, err
	}

	if current.Status != string(scenario.StatusRunning) {
		return scenario.ScenarioRun{}, fmt.Errorf(
			"%w: run %s is %s, not running; only a claimed run can finish",
			bserr.ErrConflict, in.ID, current.Status)
	}

	row, err := runFromDomain(in)
	if err != nil {
		return scenario.ScenarioRun{}, err
	}

	// The frozen fields are copied from the row that was just read, never
	// from the argument.
	//
	// Finish rewrites the whole record, manifest column included, and its
	// caller hands back a run it has been holding. Today that is always
	// Claim's own output, but a retry endpoint, an HTTP-supplied run body or
	// a stuck-run reaper would each hand back something else — and a manifest
	// arriving that way would silently replace the frozen one, leaving a
	// record that is internally consistent and still looks citable. Nothing
	// would detect it. The memory store carries the same four fields forward
	// for the same reason.
	//
	// StartedAt is among them because Claim stamps it: it is an outcome of
	// being claimed, not of executing, and a caller handing back a zero would
	// blank the claim time on a record that still reads perfectly consistent.
	row.ScenarioID, row.Manifest, row.QueuedAt = current.ScenarioID, current.Manifest, current.QueuedAt
	row.StartedAt = current.StartedAt

	version := current.Version

	if err := s.base.Update(ctx, RunEntity, in.ID, row, &version); err != nil {
		return scenario.ScenarioRun{}, err
	}

	// Mapped back from the row that was written rather than returning the
	// argument, so what the caller receives is what the store now holds.
	return row.toDomain()
}
