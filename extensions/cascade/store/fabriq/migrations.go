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
	g.MustRegister(migration0002Initiating)
	g.MustRegister(migration0003SBase)
	g.MustRegister(migration0004CanonicalHash)

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

// migration0002Initiating adds the column for run.Initiating.
//
// A second migration rather than an edit to the first. 0001 had already been
// applied to a running database by the time the field landed, and grove will
// not re-run a version it has recorded — so amending it in place would leave
// that database silently without the column.
var migration0002Initiating = &migrate.Migration{
	Name:    "cascade_initiating",
	Version: "20260803000031",
	Comment: "the outages that started each cascade",
	Up:      up0002,
	Down:    down0002,
}

// migration0003SBase adds the column for run.CascadeRun.SBaseVA.
//
// A third migration for the same reason 0002 is a second rather than an edit
// to 0001: a database that already ran 0001 and 0002 will never be handed
// them again, so the only way to add a column to an already-deployed table
// is a new migration.
var migration0003SBase = &migrate.Migration{
	Name:    "cascade_s_base",
	Version: "20260803000032",
	Comment: "the per-unit base LoadLost, Balance and Shed are stored against",
	Up:      up0003,
	Down:    down0003,
}

// migration0004CanonicalHash adds the column for
// run.CascadeRun.CanonicalHash — the network's identity as networkmodel
// stores it, beside the solver's own.
//
// A fourth migration for the reason 0002 and 0003 were new migrations rather
// than edits: grove never hands back a version it has recorded.
var migration0004CanonicalHash = &migrate.Migration{
	Name:    "cascade_canonical_hash",
	Version: "20260803000033",
	Comment: "the network's identity as networkmodel stores it, beside the solver's",
	Up:      up0004,
	Down:    down0004,
}

func up0002(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, initiatingColumn())
}

func up0004(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, canonicalHashColumn())
}

// canonicalHashColumn is the DDL, separated from the executor so a test can
// read it alongside the rest of the schema — the same split the two columns
// above use.
//
// Nullable, and pointedly unlike the two before it: 0002 and 0003 could both
// default, because an empty initiating list and a base of zero are values the
// domain already reads as "none stated". A canonical hash has no such value.
// Every run stored before this migration was recorded by a binary that never
// saw one, so the honest answer is unknown — and defaulting to the empty
// string would instead give every one of those runs the same hash, which a
// listing would then return as if they were one network.
//
// The index is partial for the same reason: a row with no canonical hash can
// never be a match, so indexing those would be indexing the platform's history.
func canonicalHashColumn() []string {
	return []string{
		`ALTER TABLE ` + RunTable + `
			ADD COLUMN IF NOT EXISTS canonical_hash TEXT`,

		`CREATE INDEX IF NOT EXISTS ` + RunTable + `_canonical_idx
			ON ` + RunTable + ` (tenant_id, canonical_hash, started_at DESC)
			WHERE canonical_hash IS NOT NULL`,
	}
}

func down0004(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, []string{
		`DROP INDEX IF EXISTS ` + RunTable + `_canonical_idx`,
		`ALTER TABLE ` + RunTable + ` DROP COLUMN IF EXISTS canonical_hash`,
	})
}

// initiatingColumn is the DDL, separated from the executor so a test can read
// it alongside the rest of the schema.
func initiatingColumn() []string {
	return []string{
		`ALTER TABLE ` + RunTable + `
			ADD COLUMN IF NOT EXISTS initiating JSONB NOT NULL DEFAULT '[]'::jsonb`,
	}
}

func down0002(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, []string{
		`ALTER TABLE ` + RunTable + ` DROP COLUMN IF EXISTS initiating`,
	})
}

func up0003(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, sBaseColumn())
}

// sBaseColumn is the DDL, separated from the executor so a test can read it
// alongside the rest of the schema — the same split initiatingColumn uses.
func sBaseColumn() []string {
	return []string{
		`ALTER TABLE ` + RunTable + `
			ADD COLUMN IF NOT EXISTS s_base_va DOUBLE PRECISION NOT NULL DEFAULT 0`,
	}
}

func down0003(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, []string{
		`ALTER TABLE ` + RunTable + ` DROP COLUMN IF EXISTS s_base_va`,
	})
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
