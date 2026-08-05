package fabriq

import (
	"context"
	"fmt"

	fabriqmigrations "github.com/xraph/fabriq/migrations"
	"github.com/xraph/grove/migrate"
)

// GroupName identifies this extension's migration group.
const GroupName = "scenario"

// Group returns the migration group, ordered after Fabriq's own.
//
// The dependency is real: Fabriq's migrations create the outbox and
// projection-state tables every write here relies on. Grove resolves the
// ordering topologically, but only when both groups are handed to one
// orchestrator — registering each extension's separately makes DependsOn a
// silent no-op.
func Group() *migrate.Group {
	g := migrate.NewGroup(GroupName, migrate.DependsOn(fabriqmigrations.GroupName))

	g.MustRegister(migration0001Tables)
	g.MustRegister(migration0002RecentRuns)
	g.MustRegister(migration0003VoltageBaseFallback)

	return g
}

// Version strings are capped at 14 characters: grove stores them in a
// VARCHAR(14), and a longer one fails at insert rather than at registration.
// The unique constraint is on (version, group), so a version matching another
// extension's is harmless — but a distinct one keeps the ordering readable.
var migration0001Tables = &migrate.Migration{
	Name:    "scenario_tables",
	Version: "20260805000010",
	Comment: "scenario and run tables with tenancy and RLS",
	Up:      up0001,
	Down:    down0001,
}

// A second migration rather than another statement in tables().
//
// migration0001Tables has run. Grove records a migration by version and skips
// one it has already applied, so an index appended to the first migration's
// DDL would be created on a fresh database and on no existing one — and the
// difference would only show up as a query plan, never as an error.
var migration0002RecentRuns = &migrate.Migration{
	Name:    "scenario_recent_runs",
	Version: "20260805000020",
	Comment: "index for listing runs across scenarios, most recent first",
	Up:      up0002,
	Down:    down0002,
}

// A third migration rather than a column added to tables(), for the reason
// 0002 is a second migration: a database that has already run 0001 will never
// be handed it again, so a column appended to that DDL would exist on a fresh
// database and on no deployed one.
var migration0003VoltageBaseFallback = &migrate.Migration{
	Name:    "scenario_voltage_base_fallback",
	Version: "20260805000030",
	Comment: "how many of a run's ratings converted against the model's own voltage base",
	Up:      up0003,
	Down:    down0003,
}

func up0001(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, tables())
}

func up0003(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, fallbackColumn())
}

// fallbackColumn is the DDL, separated from the executor so a test can read
// it alongside the rest of the schema — the same split tables() uses.
//
// Nullable, and deliberately without a DEFAULT 0. Every run stored before
// this migration was rated by a binary that did not count fallbacks, so its
// answer to "how many of these ratings rest on a substituted voltage" is
// unknown — and a default would write 0, which is not unknown but the
// opposite claim: that every branch converted against a voltage its own bus
// stated. The column exists to make a substituted voltage visible; filling it
// with a number nobody measured would make invisibility its default value.
//
// INTEGER, matching weather_rated and branch_count: it counts branches, and
// it cannot exceed weather_rated.
func fallbackColumn() []string {
	return []string{
		`ALTER TABLE ` + RunTable + `
			ADD COLUMN IF NOT EXISTS base_voltage_fallback INTEGER`,
	}
}

func down0003(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, []string{
		`ALTER TABLE ` + RunTable + ` DROP COLUMN IF EXISTS base_voltage_fallback`,
	})
}

// up0002 indexes the question RunStore.List asks.
//
// RunTable_scenario_idx cannot serve it: that index leads on scenario_id, so
// a scan naming no scenario cannot walk it in queued_at order and Postgres
// sorts the tenant's whole run table to return the newest hundred rows. Runs
// are append-only and a console polls this endpoint, so that cost grows
// without bound and is paid on every poll.
//
// id DESC is in the key rather than left to a sort step because it is the
// tie-break both stores apply, and an index that ordered only by queued_at
// would leave rows queued in the same instant to come back in whatever order
// the heap held them — which is how the two stores would silently start
// disagreeing again.
func up0002(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, []string{
		`CREATE INDEX IF NOT EXISTS ` + RunTable + `_recent_idx
			ON ` + RunTable + ` (tenant_id, queued_at DESC, id DESC)`,
	})
}

func down0002(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, []string{
		`DROP INDEX IF EXISTS ` + RunTable + `_recent_idx`,
	})
}

// tables is the DDL, separated from the executor so a test can read it
// alongside the rest of the schema.
func tables() []string {
	return []string{
		// Manifest is JSONB rather than exploded columns — see scenarioRow's
		// own comment. A scenario is authored, so Create/Get is all this
		// table needs; nothing queries by a field inside the manifest.
		`CREATE TABLE IF NOT EXISTS ` + ScenarioTable + ` (
			id          TEXT PRIMARY KEY,
			tenant_id   TEXT NOT NULL,
			version     BIGINT NOT NULL,
			name        TEXT NOT NULL,
			manifest    JSONB NOT NULL,
			created_at  TIMESTAMPTZ NOT NULL,
			updated_at  TIMESTAMPTZ NOT NULL
		)`,

		`CREATE INDEX IF NOT EXISTS ` + ScenarioTable + `_tenant_idx
			ON ` + ScenarioTable + ` (tenant_id)`,

		// Row-level security, forced. Without FORCE the table owner is exempt
		// and tenant isolation is silently a no-op for exactly the connection
		// most likely to be used by mistake.
		`ALTER TABLE ` + ScenarioTable + ` ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ` + ScenarioTable + ` FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_isolation ON ` + ScenarioTable,
		`CREATE POLICY tenant_isolation ON ` + ScenarioTable + `
			USING (tenant_id = current_setting('app.tenant_id', true))
			WITH CHECK (tenant_id = current_setting('app.tenant_id', true))`,

		// A run's own scalars stay columns: status, the child-run links and
		// the coverage counters are what a worker and a dashboard query by.
		// The frozen manifest is JSONB for the same reason ScenarioTable's
		// is — see runRow's own comment.
		`CREATE TABLE IF NOT EXISTS ` + RunTable + ` (
			id              TEXT PRIMARY KEY,
			tenant_id       TEXT NOT NULL,
			version         BIGINT NOT NULL,
			scenario_id     TEXT NOT NULL,
			manifest        JSONB NOT NULL,
			status          TEXT NOT NULL,
			failure         JSONB,
			sweep_run_id    TEXT NOT NULL DEFAULT '',
			cascade_run_id  TEXT NOT NULL DEFAULT '',
			weather_rated   INTEGER,
			branch_count    INTEGER,
			solver_version  TEXT NOT NULL DEFAULT '',
			queued_at       TIMESTAMPTZ NOT NULL,
			started_at      TIMESTAMPTZ,
			finished_at     TIMESTAMPTZ
		)`,

		`CREATE INDEX IF NOT EXISTS ` + RunTable + `_tenant_idx
			ON ` + RunTable + ` (tenant_id)`,

		// The question ListByScenario answers: every run of one scenario,
		// most recent first.
		`CREATE INDEX IF NOT EXISTS ` + RunTable + `_scenario_idx
			ON ` + RunTable + ` (tenant_id, scenario_id, queued_at DESC)`,

		// Claim's scan: the oldest queued run, without reading rows that have
		// already left the queue. Partial, because queued is the transient
		// state a run passes through — most rows will be running, complete or
		// failed, and indexing those would be most of the table.
		`CREATE INDEX IF NOT EXISTS ` + RunTable + `_queued_idx
			ON ` + RunTable + ` (tenant_id, queued_at ASC, id ASC)
			WHERE status = 'queued'`,

		`ALTER TABLE ` + RunTable + ` ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ` + RunTable + ` FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_isolation ON ` + RunTable,
		`CREATE POLICY tenant_isolation ON ` + RunTable + `
			USING (tenant_id = current_setting('app.tenant_id', true))
			WITH CHECK (tenant_id = current_setting('app.tenant_id', true))`,
	}
}

// down0001 drops what up0001 created.
//
// A migration that cannot be rolled back is one nobody can test safely, which
// is why the project requires both directions to run cleanly.
func down0001(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, []string{
		`DROP TABLE IF EXISTS ` + RunTable,
		`DROP TABLE IF EXISTS ` + ScenarioTable,
	})
}

// execAll runs statements in order, naming the one that failed.
func execAll(ctx context.Context, exec migrate.Executor, statements []string) error {
	for index, statement := range statements {
		if _, err := exec.Exec(ctx, statement); err != nil {
			return fmt.Errorf("scenario: statement %d: %w", index+1, err)
		}
	}

	return nil
}
