package fabriq

import (
	"context"
	"fmt"

	fabriqmigrations "github.com/xraph/fabriq/migrations"
	"github.com/xraph/grove/migrate"
)

// GroupName identifies this extension's migration group.
const GroupName = "contingency"

// Group returns the migration group, ordered after Fabriq's own.
func Group() *migrate.Group {
	g := migrate.NewGroup(GroupName, migrate.DependsOn(fabriqmigrations.GroupName))

	g.MustRegister(migration0001Tables)

	return g
}

var migration0001Tables = &migrate.Migration{
	Name:    "contingency_tables",
	Version: "20260802000060",
	Comment: "run table with tenancy and RLS",
	Up:      up0001,
	Down:    down0001,
}

func up0001(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, []string{
		// Candidates are JSONB rather than a child table per contingency and
		// another per violation. A sweep is written once and read whole, and a
		// 3000-branch network would otherwise mean 3000 inserts plus a
		// variable number of violation rows — nobody asks for one
		// contingency's result without the sweep it belongs to.
		`CREATE TABLE IF NOT EXISTS ` + RunTable + ` (
			id             TEXT PRIMARY KEY,
			tenant_id      TEXT NOT NULL,
			version        BIGINT NOT NULL,
			model_hash     TEXT NOT NULL,
			solver_version TEXT NOT NULL,
			started_at     TIMESTAMPTZ NOT NULL,
			finished_at    TIMESTAMPTZ NOT NULL,
			concerning     INTEGER NOT NULL DEFAULT 0,
			candidates     JSONB NOT NULL DEFAULT '[]'::jsonb
		)`,

		`CREATE INDEX IF NOT EXISTS ` + RunTable + `_tenant_idx
			ON ` + RunTable + ` (tenant_id)`,

		// The question always asked: what do we know about this network, most
		// recent first.
		`CREATE INDEX IF NOT EXISTS ` + RunTable + `_model_idx
			ON ` + RunTable + ` (tenant_id, model_hash, started_at DESC)`,

		// Which sweeps found something, without scanning every candidate
		// array. Partial, because a sweep that found nothing is the common
		// case and indexing those would be most of the table.
		`CREATE INDEX IF NOT EXISTS ` + RunTable + `_concerning_idx
			ON ` + RunTable + ` (tenant_id, started_at DESC) WHERE concerning > 0`,

		`ALTER TABLE ` + RunTable + ` ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ` + RunTable + ` FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_isolation ON ` + RunTable,
		`CREATE POLICY tenant_isolation ON ` + RunTable + `
			USING (tenant_id = current_setting('app.tenant_id', true))`,
	})
}

func down0001(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, []string{`DROP TABLE IF EXISTS ` + RunTable})
}

// execAll runs statements in order, naming the one that failed.
func execAll(ctx context.Context, exec migrate.Executor, statements []string) error {
	for index, statement := range statements {
		if _, err := exec.Exec(ctx, statement); err != nil {
			return fmt.Errorf("contingency: statement %d: %w", index+1, err)
		}
	}

	return nil
}
