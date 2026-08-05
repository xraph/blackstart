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

func up0001(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, tables())
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
