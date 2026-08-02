package fabriq

import (
	"context"
	"fmt"

	fabriqmigrations "github.com/xraph/fabriq/migrations"
	"github.com/xraph/grove/migrate"
)

// GroupName identifies this extension's migration group.
const GroupName = "geo"

// Group returns the migration group, ordered after Fabriq's own.
//
// The dependency is doubly real here. Fabriq's migrations create the outbox
// tables every write relies on, and its 0011 migration creates
// fabriq_geometries — the PostGIS table this extension's geometry lands in.
// Nothing here creates a geometry column of its own.
func Group() *migrate.Group {
	g := migrate.NewGroup(GroupName, migrate.DependsOn(fabriqmigrations.GroupName))

	g.MustRegister(migration0001Tables)

	return g
}

// Version strings are capped at 14 characters: grove stores them in a
// VARCHAR(14), and a longer one fails at insert rather than at registration.
var migration0001Tables = &migrate.Migration{
	Name:    "geo_tables",
	Version: "20260802000020",
	Comment: "route and span tables with tenancy and RLS",
	Up:      up0001,
	Down:    down0001,
}

func up0001(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, []string{
		`CREATE TABLE IF NOT EXISTS ` + RouteTable + ` (
			id          TEXT PRIMARY KEY,
			tenant_id   TEXT NOT NULL,
			version     BIGINT NOT NULL,
			branch_id   TEXT NOT NULL,
			provenance  TEXT NOT NULL,
			length_m    DOUBLE PRECISION NOT NULL
		)`,

		`CREATE INDEX IF NOT EXISTS ` + RouteTable + `_tenant_idx
			ON ` + RouteTable + ` (tenant_id)`,

		// A branch has one route, and the question asked of this table is
		// almost always "where does this branch run". Unique, because two
		// routes for one branch is a contradiction rather than a duplicate:
		// nothing could say which path the branch takes.
		`CREATE UNIQUE INDEX IF NOT EXISTS ` + RouteTable + `_branch_idx
			ON ` + RouteTable + ` (tenant_id, branch_id)`,

		// geom_wkt rather than a geometry column. The indexed geometry lives
		// in fabriq_geometries, written through Fabriq's spatial port, which
		// is a direct write outside the aggregate's transaction — so the text
		// here is the durable copy and the PostGIS column is a rebuildable
		// index over it.
		`CREATE TABLE IF NOT EXISTS ` + SpanTable + ` (
			id           TEXT PRIMARY KEY,
			tenant_id    TEXT NOT NULL,
			version      BIGINT NOT NULL,
			route_id     TEXT NOT NULL,
			sequence     INTEGER NOT NULL,
			geom_wkt     TEXT NOT NULL,
			length_m     DOUBLE PRECISION NOT NULL,
			azimuth_deg  DOUBLE PRECISION NOT NULL
		)`,

		`CREATE INDEX IF NOT EXISTS ` + SpanTable + `_tenant_idx
			ON ` + SpanTable + ` (tenant_id)`,

		// Spans are read as a whole route, in order, every time. Never one at
		// a time — a single span in isolation says nothing about the line it
		// belongs to.
		`CREATE INDEX IF NOT EXISTS ` + SpanTable + `_route_idx
			ON ` + SpanTable + ` (tenant_id, route_id, sequence)`,

		// Row-level security, forced. Without FORCE the table owner is exempt
		// and tenant isolation is silently a no-op for exactly the connection
		// most likely to be used by mistake.
		`ALTER TABLE ` + RouteTable + ` ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ` + RouteTable + ` FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_isolation ON ` + RouteTable,
		`CREATE POLICY tenant_isolation ON ` + RouteTable + `
			USING (tenant_id = current_setting('app.tenant_id', true))`,

		`ALTER TABLE ` + SpanTable + ` ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ` + SpanTable + ` FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_isolation ON ` + SpanTable,
		`CREATE POLICY tenant_isolation ON ` + SpanTable + `
			USING (tenant_id = current_setting('app.tenant_id', true))`,
	})
}

func down0001(ctx context.Context, exec migrate.Executor) error {
	// The geometry rows in fabriq_geometries are deliberately left alone: that
	// table belongs to Fabriq, is shared with every other entity that has a
	// geometry, and dropping rows from it here would make this migration's
	// down step reach outside its own group.
	return execAll(ctx, exec, []string{
		`DROP TABLE IF EXISTS ` + SpanTable,
		`DROP TABLE IF EXISTS ` + RouteTable,
	})
}

// execAll runs statements in order, naming the one that failed.
func execAll(ctx context.Context, exec migrate.Executor, statements []string) error {
	for index, statement := range statements {
		if _, err := exec.Exec(ctx, statement); err != nil {
			return fmt.Errorf("geo: statement %d: %w", index+1, err)
		}
	}

	return nil
}
