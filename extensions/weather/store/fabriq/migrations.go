package fabriq

import (
	"context"
	"fmt"

	fabriqmigrations "github.com/xraph/fabriq/migrations"
	"github.com/xraph/grove/migrate"
)

// GroupName identifies this extension's migration group.
const GroupName = "weather"

// Group returns the migration group, ordered after Fabriq's own.
//
// The dependency is real: Fabriq's migrations create the outbox and
// projection-state tables every write here relies on, and its 0011 creates
// fabriq_geometries, which station locations are indexed into. Grove resolves
// the ordering topologically, but only when both groups are handed to one
// orchestrator — running each extension's separately silently ignores
// DependsOn.
func Group() *migrate.Group {
	g := migrate.NewGroup(GroupName, migrate.DependsOn(fabriqmigrations.GroupName))

	g.MustRegister(migration0001Tables)

	return g
}

// Version strings are capped at 14 characters: grove stores them in a
// VARCHAR(14), and a longer one fails at insert rather than at registration.
var migration0001Tables = &migrate.Migration{
	Name:    "weather_tables",
	Version: "20260803000010",
	Comment: "cycle, grid and station tables plus the two sample hypertables",
	Up:      up0001,
	Down:    down0001,
}

func up0001(ctx context.Context, exec migrate.Executor) error {
	statements := make([]string, 0, 32)
	statements = append(statements, catalogTables()...)
	statements = append(statements, seriesTables()...)

	return execAll(ctx, exec, statements)
}

// catalogTables is the relational half: entities, with tenancy and RLS.
func catalogTables() []string {
	return []string{
		// Fabriq generates DDL only for dynamic entities, so a model-backed
		// entity's table is ours to write — and it must reproduce Fabriq's
		// structural contract exactly: id, tenant_id, version.
		`CREATE TABLE IF NOT EXISTS ` + CycleTable + ` (
			id         TEXT PRIMARY KEY,
			tenant_id  TEXT NOT NULL,
			version    BIGINT NOT NULL,
			feed_id    TEXT NOT NULL,
			model      TEXT NOT NULL,
			issued_at  TIMESTAMPTZ NOT NULL,
			status     TEXT NOT NULL
		)`,

		`CREATE INDEX IF NOT EXISTS ` + CycleTable + `_tenant_idx
			ON ` + CycleTable + ` (tenant_id)`,

		// One cycle per model per issuance. Unique because two would be a
		// contradiction rather than a duplicate: nothing could say which run a
		// sample came from. The constraint lives here rather than only in the
		// store, because a concurrent backfill and poll is exactly how the
		// second one gets written.
		`CREATE UNIQUE INDEX IF NOT EXISTS ` + CycleTable + `_issuance_key
			ON ` + CycleTable + ` (tenant_id, model, issued_at)`,

		// The backfill pass asks "what is the newest cycle of this model", and
		// asks it on every tick.
		`CREATE INDEX IF NOT EXISTS ` + CycleTable + `_recent_idx
			ON ` + CycleTable + ` (tenant_id, model, issued_at DESC)`,

		`CREATE TABLE IF NOT EXISTS ` + GridTable + ` (
			id                  TEXT PRIMARY KEY,
			tenant_id           TEXT NOT NULL,
			version             BIGINT NOT NULL,
			cycle_id            TEXT NOT NULL,
			forecast_hour       INTEGER NOT NULL,
			field               TEXT NOT NULL,
			blob_ref            TEXT NOT NULL,
			dataset_version_id  TEXT NOT NULL,
			nx                  INTEGER NOT NULL DEFAULT 0,
			ny                  INTEGER NOT NULL DEFAULT 0,
			projection          TEXT NOT NULL DEFAULT '',
			sampled_at          TIMESTAMPTZ,
			covered             INTEGER NOT NULL DEFAULT 0,
			total               INTEGER NOT NULL DEFAULT 0
		)`,

		`CREATE INDEX IF NOT EXISTS ` + GridTable + `_tenant_idx
			ON ` + GridTable + ` (tenant_id)`,

		// Grids are read as a whole cycle, in this order, every time. Never one
		// at a time — a single grid says nothing about the run it belongs to.
		`CREATE INDEX IF NOT EXISTS ` + GridTable + `_cycle_idx
			ON ` + GridTable + ` (tenant_id, cycle_id, forecast_hour, field)`,

		// One retrieval per field per hour per cycle. A re-fetch that produced
		// a second row would leave two dataset versions for one field with
		// nothing to say which the samples came from.
		`CREATE UNIQUE INDEX IF NOT EXISTS ` + GridTable + `_field_key
			ON ` + GridTable + ` (tenant_id, cycle_id, forecast_hour, field)`,

		`CREATE TABLE IF NOT EXISTS ` + StationTable + ` (
			id         TEXT PRIMARY KEY,
			tenant_id  TEXT NOT NULL,
			version    BIGINT NOT NULL,
			code       TEXT NOT NULL,
			name       TEXT NOT NULL DEFAULT '',
			lon        DOUBLE PRECISION NOT NULL,
			lat        DOUBLE PRECISION NOT NULL,
			elevation  DOUBLE PRECISION NOT NULL DEFAULT 0
		)`,

		`CREATE INDEX IF NOT EXISTS ` + StationTable + `_tenant_idx
			ON ` + StationTable + ` (tenant_id)`,

		// Readings arrive naming "KAUS", so the publisher's code is the lookup
		// that matters and two stations sharing one would make it ambiguous.
		`CREATE UNIQUE INDEX IF NOT EXISTS ` + StationTable + `_code_key
			ON ` + StationTable + ` (tenant_id, code)`,

		// Row-level security, forced. Without FORCE the table owner is exempt
		// and tenant isolation is silently a no-op for exactly the connection
		// most likely to be used by mistake.
		rlsFor(CycleTable),
		forceRLSFor(CycleTable),
		dropPolicyFor(CycleTable),
		policyFor(CycleTable),

		rlsFor(GridTable),
		forceRLSFor(GridTable),
		dropPolicyFor(GridTable),
		policyFor(GridTable),

		rlsFor(StationTable),
		forceRLSFor(StationTable),
		dropPolicyFor(StationTable),
		policyFor(StationTable),
	}
}

// seriesTables is the time-series half.
//
// The column list is not ours to choose. fabriq's Timescale adapter writes
// `INSERT INTO <series> (time, tenant_id, tag_id, value, quality, scope_id)`
// with those names hardcoded, so a table missing any of them fails every
// write — scope_id in particular is easy to omit and is not optional.
//
// These tables get no row-level security, which is the documented exception
// rather than an oversight: Timescale's columnstore forbids RLS. Isolation is
// structural, through the Timeseries port and through this extension's own
// discipline of writing a tenant predicate into every read. fabriq's raw-SQL
// guard does not help here — its table list comes from an unexported constant
// naming only fabriq's own demo table, so it will not cover these.
func seriesTables() []string {
	out := make([]string, 0, 8)

	for _, table := range []string{ForecastSeries, ObservationSeries} {
		out = append(out,
			`CREATE TABLE IF NOT EXISTS `+table+` (
				time       TIMESTAMPTZ NOT NULL,
				tenant_id  TEXT NOT NULL,
				tag_id     TEXT NOT NULL,
				value      DOUBLE PRECISION NOT NULL,
				quality    INT NOT NULL DEFAULT 0,
				scope_id   TEXT
			)`,

			// Every read is (tenant, series key, time window), in that order.
			`CREATE INDEX IF NOT EXISTS `+table+`_idx
				ON `+table+` (tenant_id, tag_id, time DESC)`,

			// Hypertable only where timescaledb exists, so a plain-Postgres
			// environment applies this migration cleanly and gets an ordinary
			// table with the same shape. The dev stack runs
			// timescale/timescaledb-ha, so the guarded branch is the one that
			// actually runs there.
			`DO $$
			BEGIN
				IF EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'timescaledb') THEN
					CREATE EXTENSION IF NOT EXISTS timescaledb;
					PERFORM create_hypertable('`+table+`', 'time',
						if_not_exists => TRUE, migrate_data => TRUE);
				END IF;
			END
			$$`,
		)
	}

	return out
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
// is why the project requires both directions to run cleanly. Grids go before
// cycles because they reference them.
//
// The rows in fabriq_geometries are deliberately left alone: that table
// belongs to Fabriq and is shared with every other entity that has a geometry,
// so dropping from it here would reach outside this group.
func down0001(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, []string{
		`DROP TABLE IF EXISTS ` + ObservationSeries,
		`DROP TABLE IF EXISTS ` + ForecastSeries,
		`DROP TABLE IF EXISTS ` + StationTable,
		`DROP TABLE IF EXISTS ` + GridTable,
		`DROP TABLE IF EXISTS ` + CycleTable,
	})
}

// execAll runs statements in order, naming the one that failed.
func execAll(ctx context.Context, exec migrate.Executor, statements []string) error {
	for index, statement := range statements {
		if _, err := exec.Exec(ctx, statement); err != nil {
			return fmt.Errorf(
				"weather: migration statement %d failed: %w\n%s",
				index+1, err, statement)
		}
	}

	return nil
}
