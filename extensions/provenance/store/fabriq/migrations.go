package fabriq

import (
	"context"
	"fmt"

	fabriqmigrations "github.com/xraph/fabriq/migrations"
	"github.com/xraph/grove/migrate"
)

// GroupName identifies this extension's migration group.
const GroupName = "provenance"

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
	Name:    "provenance_tables",
	Version: "20260803000020",
	Comment: "dataset, model, run and lineage tables, append-only and referentially closed",
	Up:      up0001,
	Down:    down0001,
}

// appendOnlyFunc is the trigger function every provenance table shares.
//
// One function rather than four, because the rule is one rule. The message
// names the table so a caller who trips it knows which record they tried to
// amend, and states the reason rather than the mechanism — "append-only" alone
// would read as an implementation detail somebody could ask to have relaxed.
const appendOnlyFunc = `
CREATE OR REPLACE FUNCTION provenance_refuse_amendment() RETURNS trigger AS $$
BEGIN
	RAISE EXCEPTION 'table % is append-only: a provenance record states what happened, so correcting one would mean claiming a different thing happened. Record a new one and explain it.', TG_TABLE_NAME
		USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;`

func up0001(ctx context.Context, exec migrate.Executor) error {
	statements := make([]string, 0, 48)

	statements = append(statements, appendOnlyFunc)
	statements = append(statements, datasetTable()...)
	statements = append(statements, modelTable()...)
	statements = append(statements, runTable()...)
	statements = append(statements, lineageTable()...)

	return execAll(ctx, exec, statements)
}

func datasetTable() []string {
	return []string{
		// Fabriq generates DDL only for dynamic entities, so a model-backed
		// entity's table is ours to write — and it must reproduce Fabriq's
		// structural contract exactly: id, tenant_id, version.
		//
		// No bitemporal columns. A provenance record is not corrected, so it
		// has no valid-time interval to filter on.
		`CREATE TABLE IF NOT EXISTS ` + DatasetTable + ` (
			id              TEXT PRIMARY KEY,
			tenant_id       TEXT NOT NULL,
			version         BIGINT NOT NULL,
			source_id       TEXT NOT NULL DEFAULT '',
			url             TEXT NOT NULL,
			content_hash    TEXT NOT NULL,
			size_bytes      BIGINT NOT NULL DEFAULT 0,
			parser_version  TEXT NOT NULL,
			retrieved_at    TIMESTAMPTZ NOT NULL
		)`,

		`CREATE INDEX IF NOT EXISTS ` + DatasetTable + `_tenant_idx
			ON ` + DatasetTable + ` (tenant_id)`,

		// The target of the composite foreign keys below. Redundant with the
		// primary key on its own, and that is the point: a foreign key needs a
		// unique constraint on exactly the columns it references, and the
		// tenant has to be one of them.
		`CREATE UNIQUE INDEX IF NOT EXISTS ` + DatasetTable + `_tenant_id_key
			ON ` + DatasetTable + ` (tenant_id, id)`,

		// The dedupe path every ingest calls, and the reason FindDatasetByHash
		// is a lookup rather than a scan. Unique because two retrievals of the
		// same bytes are one version — which is what makes a re-download that
		// changed nothing cheap, since it invalidates nothing derived from the
		// previous one.
		`CREATE UNIQUE INDEX IF NOT EXISTS ` + DatasetTable + `_hash_key
			ON ` + DatasetTable + ` (tenant_id, content_hash)`,

		// Row-level security is enforced rather than assumed. FORCE matters:
		// without it the table owner bypasses the policy, and the application
		// role must not be able to.
		`ALTER TABLE ` + DatasetTable + ` ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ` + DatasetTable + ` FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_isolation ON ` + DatasetTable,
		`CREATE POLICY tenant_isolation ON ` + DatasetTable + `
			USING (tenant_id = current_setting('app.tenant_id', true))
			WITH CHECK (tenant_id = current_setting('app.tenant_id', true))`,

		`DROP TRIGGER IF EXISTS ` + DatasetTable + `_append_only ON ` + DatasetTable,
		`CREATE TRIGGER ` + DatasetTable + `_append_only
			BEFORE UPDATE OR DELETE ON ` + DatasetTable + `
			FOR EACH ROW EXECUTE FUNCTION provenance_refuse_amendment()`,
	}
}

func modelTable() []string {
	return []string{
		// producer_version, not version: the latter is Fabriq's structural
		// column, a BIGINT the executor stamps.
		//
		// No uniqueness on (kind, name, producer_version). RecordModel does
		// not deduplicate, and a unique key would reject re-recording a solver
		// version that is legitimately seen again on the next deploy.
		`CREATE TABLE IF NOT EXISTS ` + ModelTable + ` (
			id                TEXT PRIMARY KEY,
			tenant_id         TEXT NOT NULL,
			version           BIGINT NOT NULL,
			kind              TEXT NOT NULL,
			name              TEXT NOT NULL,
			producer_version  TEXT NOT NULL,
			code_commit       TEXT NOT NULL,
			parameters        JSONB NOT NULL DEFAULT '{}'::jsonb,
			recorded_at       TIMESTAMPTZ NOT NULL
		)`,

		`CREATE INDEX IF NOT EXISTS ` + ModelTable + `_tenant_idx
			ON ` + ModelTable + ` (tenant_id)`,

		`CREATE UNIQUE INDEX IF NOT EXISTS ` + ModelTable + `_tenant_id_key
			ON ` + ModelTable + ` (tenant_id, id)`,

		`ALTER TABLE ` + ModelTable + ` ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ` + ModelTable + ` FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_isolation ON ` + ModelTable,
		`CREATE POLICY tenant_isolation ON ` + ModelTable + `
			USING (tenant_id = current_setting('app.tenant_id', true))
			WITH CHECK (tenant_id = current_setting('app.tenant_id', true))`,

		`DROP TRIGGER IF EXISTS ` + ModelTable + `_append_only ON ` + ModelTable,
		`CREATE TRIGGER ` + ModelTable + `_append_only
			BEFORE UPDATE OR DELETE ON ` + ModelTable + `
			FOR EACH ROW EXECUTE FUNCTION provenance_refuse_amendment()`,
	}
}

func runTable() []string {
	return []string{
		// finished_at is nullable: Run.Validate permits a zero FinishedAt, and
		// a zero time.Time in a NOT NULL column stores a year-1 timestamp that
		// reads as data rather than as absence.
		//
		// The foreign key is composite over (tenant_id, model_version_id).
		// PostgreSQL runs foreign key checks as internal system triggers,
		// which bypass row-level security — so a simple REFERENCES
		// blackstart_provenance_model(id) would happily let one tenant's run
		// reference another tenant's producer, reintroducing through the
		// constraint exactly the leak RLS exists to prevent.
		`CREATE TABLE IF NOT EXISTS ` + RunTable + ` (
			id                TEXT PRIMARY KEY,
			tenant_id         TEXT NOT NULL,
			version           BIGINT NOT NULL,
			model_version_id  TEXT NOT NULL,
			kind              TEXT NOT NULL,
			result_hash       TEXT NOT NULL DEFAULT '',
			seed              TEXT NOT NULL DEFAULT '',
			started_at        TIMESTAMPTZ NOT NULL,
			finished_at       TIMESTAMPTZ,
			CONSTRAINT ` + RunTable + `_model_fk
				FOREIGN KEY (tenant_id, model_version_id)
				REFERENCES ` + ModelTable + ` (tenant_id, id)
		)`,

		`CREATE INDEX IF NOT EXISTS ` + RunTable + `_tenant_idx
			ON ` + RunTable + ` (tenant_id)`,

		`CREATE UNIQUE INDEX IF NOT EXISTS ` + RunTable + `_tenant_id_key
			ON ` + RunTable + ` (tenant_id, id)`,

		// The column the COMPUTED_BY edge follows, and the one "what has this
		// solver build produced" reads.
		`CREATE INDEX IF NOT EXISTS ` + RunTable + `_model_idx
			ON ` + RunTable + ` (tenant_id, model_version_id)`,

		`ALTER TABLE ` + RunTable + ` ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ` + RunTable + ` FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_isolation ON ` + RunTable,
		`CREATE POLICY tenant_isolation ON ` + RunTable + `
			USING (tenant_id = current_setting('app.tenant_id', true))
			WITH CHECK (tenant_id = current_setting('app.tenant_id', true))`,

		`DROP TRIGGER IF EXISTS ` + RunTable + `_append_only ON ` + RunTable,
		`CREATE TRIGGER ` + RunTable + `_append_only
			BEFORE UPDATE OR DELETE ON ` + RunTable + `
			FOR EACH ROW EXECUTE FUNCTION provenance_refuse_amendment()`,
	}
}

func lineageTable() []string {
	return []string{
		// Both ends are foreign keys. An edge to a record that was never
		// written is a dangling reference in the one structure whose whole
		// value is that it can be followed — and a broken chain is worse than
		// a missing one, because it looks traceable.
		//
		// No uniqueness on (run_id, dataset_version_id, role). The in-memory
		// store permits a repeated edge, and matching it matters more than
		// tidiness: the engine is tested against that store.
		`CREATE TABLE IF NOT EXISTS ` + LineageTable + ` (
			id                  TEXT PRIMARY KEY,
			tenant_id           TEXT NOT NULL,
			version             BIGINT NOT NULL,
			run_id              TEXT NOT NULL,
			dataset_version_id  TEXT NOT NULL,
			role                TEXT NOT NULL,
			relation            TEXT NOT NULL,
			recorded_at         TIMESTAMPTZ NOT NULL,
			CONSTRAINT ` + LineageTable + `_run_fk
				FOREIGN KEY (tenant_id, run_id)
				REFERENCES ` + RunTable + ` (tenant_id, id),
			CONSTRAINT ` + LineageTable + `_dataset_fk
				FOREIGN KEY (tenant_id, dataset_version_id)
				REFERENCES ` + DatasetTable + ` (tenant_id, id)
		)`,

		`CREATE INDEX IF NOT EXISTS ` + LineageTable + `_tenant_idx
			ON ` + LineageTable + ` (tenant_id)`,

		// What LineageOf reads, and therefore what "what fed this number" asks
		// on every trace.
		`CREATE INDEX IF NOT EXISTS ` + LineageTable + `_run_idx
			ON ` + LineageTable + ` (tenant_id, run_id)`,

		// The reverse question — "what has this dataset version fed" — is how
		// the blast radius of a corrected upstream release is found.
		`CREATE INDEX IF NOT EXISTS ` + LineageTable + `_dataset_idx
			ON ` + LineageTable + ` (tenant_id, dataset_version_id)`,

		`ALTER TABLE ` + LineageTable + ` ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ` + LineageTable + ` FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_isolation ON ` + LineageTable,
		`CREATE POLICY tenant_isolation ON ` + LineageTable + `
			USING (tenant_id = current_setting('app.tenant_id', true))
			WITH CHECK (tenant_id = current_setting('app.tenant_id', true))`,

		`DROP TRIGGER IF EXISTS ` + LineageTable + `_append_only ON ` + LineageTable,
		`CREATE TRIGGER ` + LineageTable + `_append_only
			BEFORE UPDATE OR DELETE ON ` + LineageTable + `
			FOR EACH ROW EXECUTE FUNCTION provenance_refuse_amendment()`,
	}
}

// down0001 drops what up0001 created.
//
// Children before parents, and the shared function last: a DROP TABLE takes
// its triggers with it, but the function outlives them and would collide with
// the next up. Dropping it while a trigger still referenced it would fail, so
// the order is not cosmetic.
func down0001(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, []string{
		`DROP TABLE IF EXISTS ` + LineageTable,
		`DROP TABLE IF EXISTS ` + RunTable,
		`DROP TABLE IF EXISTS ` + DatasetTable,
		`DROP TABLE IF EXISTS ` + ModelTable,
		`DROP FUNCTION IF EXISTS provenance_refuse_amendment()`,
	})
}

func execAll(ctx context.Context, exec migrate.Executor, stmts []string) error {
	for _, stmt := range stmts {
		if _, err := exec.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("provenance: migration statement failed: %w\n%s", err, stmt)
		}
	}

	return nil
}
