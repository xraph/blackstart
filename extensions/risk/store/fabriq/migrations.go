package fabriq

import (
	"context"
	"fmt"

	fabriqmigrations "github.com/xraph/fabriq/migrations"
	"github.com/xraph/grove/migrate"
)

// GroupName identifies this extension's migration group.
const GroupName = "risk"

// Group returns the migration group, ordered after Fabriq's own.
//
// The dependency is real: Fabriq's migrations create the outbox and
// projection-state tables every write here relies on. Grove resolves the
// ordering topologically, but only when both groups are handed to one
// orchestrator — running each extension's separately silently ignores
// DependsOn.
func Group() *migrate.Group {
	g := migrate.NewGroup(GroupName, migrate.DependsOn(fabriqmigrations.GroupName))

	g.MustRegister(migration0001Values)

	return g
}

// Version strings are capped at 14 characters: grove stores them in a
// VARCHAR(14), and a longer one fails at insert rather than at registration.
var migration0001Values = &migrate.Migration{
	Name:    "risk_values",
	Version: "20260805000010",
	Comment: "per-component failure probability series",
	Up:      up0001,
	Down:    down0001,
}

func up0001(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, seriesTable())
}

// seriesTable is the value row, plus the index its only query needs and the
// tenancy every table in this codebase enforces.
//
// id carries no PRIMARY KEY. TimescaleDB requires a hypertable's unique
// constraints to include the partitioning column (valid_at here), and
// Fabriq's write path never asked for one in the first place — model-backed
// creates are a plain INSERT, not an upsert, and CurrentVersion looks the row
// up through an ordinary index rather than a constraint.
func seriesTable() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS ` + ValueSeries + ` (
			id            TEXT NOT NULL,
			tenant_id     TEXT NOT NULL,
			version       BIGINT NOT NULL,
			component_id  TEXT NOT NULL,
			quantity      TEXT NOT NULL,
			issued_at     TIMESTAMPTZ NOT NULL,
			valid_at      TIMESTAMPTZ NOT NULL,
			probability   DOUBLE PRECISION NOT NULL,
			scalar        DOUBLE PRECISION NOT NULL DEFAULT 1
		)`,

		// Fabriq's CurrentVersion looks a row up by id alone before every
		// write; without this index that lookup is a sequential scan of the
		// whole hypertable.
		`CREATE INDEX IF NOT EXISTS ` + ValueSeries + `_id_idx
			ON ` + ValueSeries + ` (id)`,

		// Exactly the ForComponent query: tenant, component and quantity
		// narrow it to one series, issuance pins the knowledge-time axis, and
		// valid_at is left last so the planner can range-scan the window.
		`CREATE INDEX IF NOT EXISTS ` + ValueSeries + `_idx
			ON ` + ValueSeries + ` (tenant_id, component_id, quantity, issued_at, valid_at)`,

		// Row-level security, forced. Without FORCE the table owner is exempt
		// and tenant isolation is silently a no-op for exactly the connection
		// most likely to be used by mistake. Unlike hazard's and weather's
		// series tables, nothing here exempts this one, and that must stay
		// true: this hypertable must never gain a compression policy.
		// Timescale's columnstore forbids RLS outright, so compressing it
		// later would silently turn FORCE back into a no-op with no error at
		// the ALTER TABLE that did it — an invariant this migration relies on
		// but cannot itself enforce, since nothing stops a future migration
		// from adding add_compression_policy to this table.
		rlsFor(ValueSeries),
		forceRLSFor(ValueSeries),
		dropPolicyFor(ValueSeries),
		policyFor(ValueSeries),

		// Hypertable only where TimescaleDB exists, so a plain-Postgres
		// environment applies this migration cleanly and gets an ordinary
		// table with the same shape rather than a failed migration.
		`DO $$
		BEGIN
			IF EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'timescaledb') THEN
				CREATE EXTENSION IF NOT EXISTS timescaledb;
				PERFORM create_hypertable('` + ValueSeries + `', 'valid_at',
					if_not_exists => TRUE, migrate_data => TRUE);
			END IF;
		END
		$$`,
	}
}

func rlsFor(table string) string {
	return `ALTER TABLE ` + table + ` ENABLE ROW LEVEL SECURITY`
}

func forceRLSFor(table string) string {
	return `ALTER TABLE ` + table + ` FORCE ROW LEVEL SECURITY`
}

func dropPolicyFor(table string) string {
	return `DROP POLICY IF EXISTS tenant_isolation ON ` + table
}

func policyFor(table string) string {
	return `CREATE POLICY tenant_isolation ON ` + table + `
		USING (tenant_id = current_setting('app.tenant_id', true))
		WITH CHECK (tenant_id = current_setting('app.tenant_id', true))`
}

// down0001 drops what up0001 created.
//
// A migration that cannot be rolled back is one nobody can test safely, which
// is why the project requires both directions to run cleanly.
func down0001(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, []string{
		`DROP TABLE IF EXISTS ` + ValueSeries,
	})
}

// execAll runs statements in order, naming the one that failed.
func execAll(ctx context.Context, exec migrate.Executor, statements []string) error {
	for index, statement := range statements {
		if _, err := exec.Exec(ctx, statement); err != nil {
			return fmt.Errorf(
				"risk: migration statement %d failed: %w\n%s",
				index+1, err, statement)
		}
	}

	return nil
}
