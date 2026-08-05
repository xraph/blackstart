package fabriq

import (
	"context"
	"fmt"

	fabriqmigrations "github.com/xraph/fabriq/migrations"
	"github.com/xraph/grove/migrate"
)

// GroupName identifies this extension's migration group.
const GroupName = "hazard"

// Group returns the migration group, ordered after Fabriq's own.
//
// The dependency is real: Fabriq's migrations create the outbox and
// projection-state tables every write here relies on. Grove resolves the
// ordering topologically, but only when both groups are handed to one
// orchestrator — running each extension's separately silently ignores
// DependsOn.
func Group() *migrate.Group {
	g := migrate.NewGroup(GroupName, migrate.DependsOn(fabriqmigrations.GroupName))

	g.MustRegister(migration0001Tables)
	g.MustRegister(migration0002Carrier)

	return g
}

// Version strings are capped at 14 characters: grove stores them in a
// VARCHAR(14), and a longer one fails at insert rather than at registration.
var migration0001Tables = &migrate.Migration{
	Name:    "hazard_tables",
	Version: "20260803000020",
	Comment: "assignment and computation tables plus the span value hypertable",
	Up:      up0001,
	Down:    down0001,
}

// A database created before assignments keyed on the carrier has the column
// under its old name. Renamed rather than added-and-backfilled: the values are
// unchanged — a branch identifier is a carrier identifier — and a rename keeps
// every existing assignment, including the surveyed ones nobody can recreate.
//
// Guarded on the old column still existing, so a database created fresh from
// migration 0001 above (which already writes carrier_id) skips it rather than
// failing.
var migration0002Carrier = &migrate.Migration{
	Name:    "hazard_carrier",
	Version: "20260805000010",
	Comment: "assignments key on the carrier, which may be a circuit",
	Up:      up0002,
	Down:    down0002,
}

func up0002(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, renameAssignmentKey("branch_id", "carrier_id", "_branch_key", "_carrier_key"))
}

func down0002(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, renameAssignmentKey("carrier_id", "branch_id", "_carrier_key", "_branch_key"))
}

// renameAssignmentKey moves the assignment key column and its unique index.
//
// Both statements are conditional on the source existing. Grove runs a group's
// migrations in order against whatever the database already has, and a fresh
// install has the new names from 0001 — so an unconditional rename would fail
// every first-time deployment.
func renameAssignmentKey(from, to, fromIndex, toIndex string) []string {
	return []string{
		`DO $$ BEGIN
			IF EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = '` + AssignmentTable + `' AND column_name = '` + from + `'
			) THEN
				ALTER TABLE ` + AssignmentTable + ` RENAME COLUMN ` + from + ` TO ` + to + `;
			END IF;
		END $$`,

		`ALTER INDEX IF EXISTS ` + AssignmentTable + fromIndex +
			` RENAME TO ` + AssignmentTable + toIndex,
	}
}

func up0001(ctx context.Context, exec migrate.Executor) error {
	statements := make([]string, 0, 24)
	statements = append(statements, catalogTables()...)
	statements = append(statements, seriesTable()...)

	return execAll(ctx, exec, statements)
}

// catalogTables is the relational half: entities, with tenancy and RLS.
func catalogTables() []string {
	return []string{
		// Fabriq generates DDL only for dynamic entities, so a model-backed
		// entity's table is ours to write — and it must reproduce Fabriq's
		// structural contract exactly: id, tenant_id, version.
		`CREATE TABLE IF NOT EXISTS ` + AssignmentTable + ` (
			id              TEXT PRIMARY KEY,
			tenant_id       TEXT NOT NULL,
			version         BIGINT NOT NULL,
			carrier_id      TEXT NOT NULL,
			conductor_code  TEXT NOT NULL,
			basis           TEXT NOT NULL,
			note            TEXT NOT NULL DEFAULT ''
		)`,

		`CREATE INDEX IF NOT EXISTS ` + AssignmentTable + `_tenant_idx
			ON ` + AssignmentTable + ` (tenant_id)`,

		// One conductor per carrier. Unique because two would be a contradiction
		// rather than a duplicate: nothing could say which one a rating was
		// computed against. The constraint lives here rather than only in the
		// store, because a survey landing while a bulk assumption pass runs is
		// exactly how the second one gets written.
		`CREATE UNIQUE INDEX IF NOT EXISTS ` + AssignmentTable + `_carrier_key
			ON ` + AssignmentTable + ` (tenant_id, carrier_id)`,

		`CREATE TABLE IF NOT EXISTS ` + ComputationTable + ` (
			id           TEXT PRIMARY KEY,
			tenant_id    TEXT NOT NULL,
			version      BIGINT NOT NULL,
			cycle_id     TEXT NOT NULL,
			run_id       TEXT NOT NULL DEFAULT '',
			computed_at  TIMESTAMPTZ NOT NULL,
			spans        INTEGER NOT NULL DEFAULT 0,
			unrated      INTEGER NOT NULL DEFAULT 0,
			value_count  INTEGER NOT NULL DEFAULT 0
		)`,

		`CREATE INDEX IF NOT EXISTS ` + ComputationTable + `_tenant_idx
			ON ` + ComputationTable + ` (tenant_id)`,

		// Passes are read per cycle, newest first, which is also how the
		// listing walks them.
		`CREATE INDEX IF NOT EXISTS ` + ComputationTable + `_cycle_idx
			ON ` + ComputationTable + ` (tenant_id, cycle_id, computed_at DESC)`,

		// No unique index on cycle_id: several passes over one cycle is the
		// normal case, one per correction to the conductor assignments, and
		// every one is kept so that a rating already consumed downstream still
		// has the computation that made it.

		// Row-level security, forced. Without FORCE the table owner is exempt
		// and tenant isolation is silently a no-op for exactly the connection
		// most likely to be used by mistake.
		rlsFor(AssignmentTable),
		forceRLSFor(AssignmentTable),
		dropPolicyFor(AssignmentTable),
		policyFor(AssignmentTable),

		rlsFor(ComputationTable),
		forceRLSFor(ComputationTable),
		dropPolicyFor(ComputationTable),
		policyFor(ComputationTable),
	}
}

// seriesTable is the time-series half.
//
// The column list is not ours to choose. fabriq's Timescale adapter writes
// `INSERT INTO <series> (time, tenant_id, tag_id, value, quality, scope_id)`
// with those names hardcoded, so a table missing any of them fails every
// write — scope_id in particular is easy to omit and is not optional.
//
// This table gets no row-level security, which is the documented exception
// rather than an oversight: Timescale's columnstore forbids RLS. Isolation is
// structural, through the Timeseries port and through this extension's own
// discipline of writing a tenant predicate into every read. fabriq's raw-SQL
// guard does not help here — its table list comes from an unexported constant
// naming only fabriq's own demo table, so it will not cover this one.
func seriesTable() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS ` + ValueSeries + ` (
			time       TIMESTAMPTZ NOT NULL,
			tenant_id  TEXT NOT NULL,
			tag_id     TEXT NOT NULL,
			value      DOUBLE PRECISION NOT NULL,
			quality    INT NOT NULL DEFAULT 0,
			scope_id   TEXT
		)`,

		// Every read is (tenant, series key, time window), in that order.
		`CREATE INDEX IF NOT EXISTS ` + ValueSeries + `_idx
			ON ` + ValueSeries + ` (tenant_id, tag_id, time DESC)`,

		// Hypertable only where timescaledb exists, so a plain-Postgres
		// environment applies this migration cleanly and gets an ordinary
		// table with the same shape. The dev stack runs
		// timescale/timescaledb-ha, so the guarded branch is the one that
		// actually runs there.
		`DO $$
		BEGIN
			IF EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'timescaledb') THEN
				CREATE EXTENSION IF NOT EXISTS timescaledb;
				PERFORM create_hypertable('` + ValueSeries + `', 'time',
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
		`DROP TABLE IF EXISTS ` + ComputationTable,
		`DROP TABLE IF EXISTS ` + AssignmentTable,
	})
}

// execAll runs statements in order, naming the one that failed.
func execAll(ctx context.Context, exec migrate.Executor, statements []string) error {
	for index, statement := range statements {
		if _, err := exec.Exec(ctx, statement); err != nil {
			return fmt.Errorf(
				"hazard: migration statement %d failed: %w\n%s",
				index+1, err, statement)
		}
	}

	return nil
}
