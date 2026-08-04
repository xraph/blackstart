package fabriq

import (
	"context"
	"fmt"

	fabriqmigrations "github.com/xraph/fabriq/migrations"
	"github.com/xraph/grove/migrate"
)

// GroupName identifies this extension's migration group.
const GroupName = "cascade"

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
	Name:    "cascade_tables",
	Version: "20260803000030",
	Comment: "cascade run table with tenancy and RLS",
	Up:      up0001,
	Down:    down0001,
}

func up0001(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, tables())
}

// tables is the DDL, separated from the executor so a test can read it.
func tables() []string {
	return []string{
		// Events and islands are JSONB rather than child tables. A run is
		// written once and read whole — the API serves a cascade, never one
		// event of one — and a long cascade would otherwise mean a few hundred
		// inserts for a query nobody makes.
		//
		// The run's own scalars stay columns. Burying an outcome in JSON would
		// make "which cascades collapsed" a scan of every event array.
		`CREATE TABLE IF NOT EXISTS ` + RunTable + ` (
			id             TEXT PRIMARY KEY,
			tenant_id      TEXT NOT NULL,
			version        BIGINT NOT NULL,
			model_hash     TEXT NOT NULL,
			solver_version TEXT NOT NULL,
			outcome        TEXT NOT NULL,
			load_lost      DOUBLE PRECISION NOT NULL DEFAULT 0,
			steps          BIGINT NOT NULL DEFAULT 0,
			elapsed_s      DOUBLE PRECISION NOT NULL DEFAULT 0,
			started_at     TIMESTAMPTZ NOT NULL,
			finished_at    TIMESTAMPTZ NOT NULL,
			events         JSONB NOT NULL DEFAULT '[]'::jsonb,
			islands        JSONB NOT NULL DEFAULT '[]'::jsonb
		)`,

		`CREATE INDEX IF NOT EXISTS ` + RunTable + `_tenant_idx
			ON ` + RunTable + ` (tenant_id)`,

		// The question always asked: what happened to this network, most
		// recent first. Identifier is not in the index — the tie-break in the
		// query sorts within one instant, which is a handful of rows.
		`CREATE INDEX IF NOT EXISTS ` + RunTable + `_model_idx
			ON ` + RunTable + ` (tenant_id, model_hash, started_at DESC)`,

		// Which cascades did not settle, without reading every row. Partial,
		// because a settled cascade is the common case and indexing those
		// would be most of the table.
		`CREATE INDEX IF NOT EXISTS ` + RunTable + `_severe_idx
			ON ` + RunTable + ` (tenant_id, started_at DESC)
			WHERE outcome <> 'settled' OR load_lost > 0`,

		// Row-level security, forced. Without FORCE the table owner is exempt
		// and tenant isolation is silently a no-op for exactly the connection
		// most likely to be used by mistake.
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
	return execAll(ctx, exec, []string{`DROP TABLE IF EXISTS ` + RunTable})
}

// execAll runs statements in order, naming the one that failed.
func execAll(ctx context.Context, exec migrate.Executor, statements []string) error {
	for index, statement := range statements {
		if _, err := exec.Exec(ctx, statement); err != nil {
			return fmt.Errorf("cascade: statement %d: %w", index+1, err)
		}
	}

	return nil
}
