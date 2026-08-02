package fabriq

import (
	"context"
	"fmt"

	fabriqmigrations "github.com/xraph/fabriq/migrations"
	"github.com/xraph/grove/migrate"
)

// GroupName identifies this extension's migration group.
const GroupName = "datasource"

// Group returns the migration group, ordered after Fabriq's own.
//
// The dependency is real: Fabriq's migrations create the outbox and
// projection-state tables that every write here relies on. Grove resolves the
// ordering topologically, but only when both groups are handed to one
// orchestrator — running each extension's separately silently ignores
// DependsOn, which grove's own source notes.
func Group() *migrate.Group {
	g := migrate.NewGroup(GroupName, migrate.DependsOn(fabriqmigrations.GroupName))

	g.MustRegister(migration0001Tables)

	return g
}

// Version strings are capped at 14 characters: grove stores them in a
// VARCHAR(14), and a longer one fails at insert rather than at registration.
var migration0001Tables = &migrate.Migration{
	Name:    "datasource_tables",
	Version: "20260802000001",
	Comment: "source and feed tables with tenancy, RLS and bitemporal columns",
	Up:      up0001,
	Down:    down0001,
}

func up0001(ctx context.Context, exec migrate.Executor) error {
	// Every statement is raw DDL. Grove's migration documentation shows an
	// exec.NewCreateTable builder; it does not exist — the Executor interface
	// is Exec and Query only, and following the docs does not compile.
	return execAll(ctx, exec, []string{
		// Fabriq generates DDL only for dynamic entities, so a model-backed
		// entity's table is ours to write — and it must reproduce Fabriq's
		// structural contract exactly: id, tenant_id, version.
		`CREATE TABLE IF NOT EXISTS ` + SourceTable + ` (
			id                    TEXT PRIMARY KEY,
			tenant_id             TEXT NOT NULL,
			version               BIGINT NOT NULL,
			slug                  TEXT NOT NULL,
			name                  TEXT NOT NULL,
			kind                  TEXT NOT NULL,
			provides              TEXT NOT NULL DEFAULT '',
			homepage              TEXT NOT NULL DEFAULT '',
			license               TEXT NOT NULL,
			requires_attribution  BOOLEAN NOT NULL DEFAULT false
		)`,

		`CREATE INDEX IF NOT EXISTS ` + SourceTable + `_tenant_idx
			ON ` + SourceTable + ` (tenant_id)`,

		// A slug is a public handle typed on the command line, so the
		// uniqueness constraint lives in the database rather than only in the
		// store: two sources sharing one would make `--source noaa` ambiguous,
		// and a concurrent write is exactly how that happens.
		`CREATE UNIQUE INDEX IF NOT EXISTS ` + SourceTable + `_slug_key
			ON ` + SourceTable + ` (tenant_id, slug)`,

		// Row-level security is enforced rather than assumed. FORCE matters:
		// without it the table owner bypasses the policy, and the application
		// role must not be able to.
		`ALTER TABLE ` + SourceTable + ` ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ` + SourceTable + ` FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_isolation ON ` + SourceTable,
		`CREATE POLICY tenant_isolation ON ` + SourceTable + `
			USING (tenant_id = current_setting('app.tenant_id', true))
			WITH CHECK (tenant_id = current_setting('app.tenant_id', true))`,

		`CREATE TABLE IF NOT EXISTS ` + FeedTable + ` (
			id         TEXT PRIMARY KEY,
			tenant_id  TEXT NOT NULL,
			version    BIGINT NOT NULL,
			source_id  TEXT NOT NULL,
			slug       TEXT NOT NULL,
			name       TEXT NOT NULL,
			url        TEXT NOT NULL,
			format     TEXT NOT NULL,
			cadence    TEXT NOT NULL DEFAULT ''
		)`,

		`CREATE INDEX IF NOT EXISTS ` + FeedTable + `_tenant_idx
			ON ` + FeedTable + ` (tenant_id)`,

		// The column the graph projection follows, so it is worth an index of
		// its own for the "feeds of this source" read.
		`CREATE INDEX IF NOT EXISTS ` + FeedTable + `_source_idx
			ON ` + FeedTable + ` (tenant_id, source_id)`,

		// Feed slugs are unique within a source, not globally: "hrrr" under
		// NOAA and "hrrr" under a mirror are different feeds.
		`CREATE UNIQUE INDEX IF NOT EXISTS ` + FeedTable + `_slug_key
			ON ` + FeedTable + ` (tenant_id, source_id, slug)`,

		`ALTER TABLE ` + FeedTable + ` ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ` + FeedTable + ` FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_isolation ON ` + FeedTable,
		`CREATE POLICY tenant_isolation ON ` + FeedTable + `
			USING (tenant_id = current_setting('app.tenant_id', true))
			WITH CHECK (tenant_id = current_setting('app.tenant_id', true))`,
	})
}

// down0001 drops what up0001 created.
//
// A migration that cannot be rolled back is a migration nobody can test
// safely, which is why the project requires both directions to run cleanly.
// Feeds go first: they reference sources.
func down0001(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, []string{
		`DROP TABLE IF EXISTS ` + FeedTable,
		`DROP TABLE IF EXISTS ` + SourceTable,
	})
}

func execAll(ctx context.Context, exec migrate.Executor, stmts []string) error {
	for _, stmt := range stmts {
		if _, err := exec.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("datasource: migration statement failed: %w\n%s", err, stmt)
		}
	}

	return nil
}
