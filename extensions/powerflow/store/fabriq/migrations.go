package fabriq

import (
	"context"
	"fmt"

	fabriqmigrations "github.com/xraph/fabriq/migrations"
	"github.com/xraph/grove/migrate"
)

// GroupName identifies this extension's migration group.
const GroupName = "powerflow"

// Group returns the migration group, ordered after Fabriq's own.
func Group() *migrate.Group {
	g := migrate.NewGroup(GroupName, migrate.DependsOn(fabriqmigrations.GroupName))

	g.MustRegister(migration0001Tables)

	return g
}

var migration0001Tables = &migrate.Migration{
	Name:    "powerflow_tables",
	Version: "20260802000050",
	Comment: "solution table with tenancy and RLS",
	Up:      up0001,
	Down:    down0001,
}

func up0001(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, []string{
		// The per-bus and per-branch answers are JSONB rather than child
		// tables. They are written once and read whole — nothing asks for one
		// bus's voltage without the rest — and a child table would mean 2000
		// inserts per solve to support a query nobody makes.
		`CREATE TABLE IF NOT EXISTS ` + SolutionTable + ` (
			id                       TEXT PRIMARY KEY,
			tenant_id                TEXT NOT NULL,
			version                  BIGINT NOT NULL,
			model_hash               TEXT NOT NULL,
			method                   TEXT NOT NULL,
			solver_version           TEXT NOT NULL,
			enforced_reactive_limits BOOLEAN NOT NULL DEFAULT FALSE,
			iterations               BIGINT NOT NULL DEFAULT 0,
			residual                 DOUBLE PRECISION NOT NULL DEFAULT 0,
			solved_at                TIMESTAMPTZ NOT NULL,
			buses                    JSONB NOT NULL DEFAULT '[]'::jsonb,
			branches                 JSONB NOT NULL DEFAULT '[]'::jsonb,
			binding_generators       JSONB NOT NULL DEFAULT '[]'::jsonb
		)`,

		`CREATE INDEX IF NOT EXISTS ` + SolutionTable + `_tenant_idx
			ON ` + SolutionTable + ` (tenant_id)`,

		// The question always asked: what do we know about this network, most
		// recent first.
		`CREATE INDEX IF NOT EXISTS ` + SolutionTable + `_model_idx
			ON ` + SolutionTable + ` (tenant_id, model_hash, solved_at DESC)`,

		`ALTER TABLE ` + SolutionTable + ` ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ` + SolutionTable + ` FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_isolation ON ` + SolutionTable,
		`CREATE POLICY tenant_isolation ON ` + SolutionTable + `
			USING (tenant_id = current_setting('app.tenant_id', true))`,
	})
}

func down0001(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, []string{`DROP TABLE IF EXISTS ` + SolutionTable})
}

// execAll runs statements in order, naming the one that failed.
func execAll(ctx context.Context, exec migrate.Executor, statements []string) error {
	for index, statement := range statements {
		if _, err := exec.Exec(ctx, statement); err != nil {
			return fmt.Errorf("powerflow: statement %d: %w", index+1, err)
		}
	}

	return nil
}
