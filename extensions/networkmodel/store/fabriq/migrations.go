package fabriq

import (
	"context"
	"fmt"

	fabriqmigrations "github.com/xraph/fabriq/migrations"
	"github.com/xraph/grove/migrate"
)

// GroupName identifies this extension's migration group.
const GroupName = "networkmodel"

// Group returns the migration group, ordered after Fabriq's own.
//
// The dependency is real: Fabriq's migrations create the outbox and
// projection-state tables every write here relies on. Grove resolves ordering
// topologically, but only when both groups are handed to one orchestrator —
// running each extension's separately ignores DependsOn silently.
func Group() *migrate.Group {
	g := migrate.NewGroup(GroupName, migrate.DependsOn(fabriqmigrations.GroupName))

	g.MustRegister(migration0001Tables)
	g.MustRegister(migration0002BytePlane)
	g.MustRegister(migration0003Identity)

	return g
}

// Version strings are capped at 14 characters: grove stores them in a
// VARCHAR(14), and a longer one fails at insert rather than at registration.
var migration0001Tables = &migrate.Migration{
	Name:    "networkmodel_tables",
	Version: "20260802000010",
	Comment: "network model and canonical blob tables with tenancy and RLS",
	Up:      up0001,
	Down:    down0001,
}

func up0001(ctx context.Context, exec migrate.Executor) error {
	// Raw DDL. Grove's migration documentation shows an exec.NewCreateTable
	// builder; it does not exist — the Executor interface is Exec and Query
	// only, and following the docs does not compile.
	return execAll(ctx, exec, []string{
		// Fabriq generates DDL only for dynamic entities, so a model-backed
		// entity's table is ours to write, and it must reproduce Fabriq's
		// structural contract exactly: id, tenant_id, version.
		`CREATE TABLE IF NOT EXISTS ` + ModelTable + ` (
			id                  TEXT PRIMARY KEY,
			tenant_id           TEXT NOT NULL,
			version             BIGINT NOT NULL,
			content_hash        TEXT NOT NULL,
			name                TEXT NOT NULL DEFAULT '',
			source_kind         TEXT NOT NULL,
			dataset_version_id  TEXT NOT NULL DEFAULT '',
			bus_count           INTEGER NOT NULL,
			branch_count        INTEGER NOT NULL,
			built_at            TIMESTAMPTZ NOT NULL
		)`,

		`CREATE INDEX IF NOT EXISTS ` + ModelTable + `_tenant_idx
			ON ` + ModelTable + ` (tenant_id)`,

		// The question every import asks first is "do we already have this
		// network", and it asks by hash. Not unique: two models can share a
		// hash legitimately — the same network imported under two names — and
		// a unique constraint would turn that into an error rather than the
		// deduplication it is.
		`CREATE INDEX IF NOT EXISTS ` + ModelTable + `_hash_idx
			ON ` + ModelTable + ` (tenant_id, content_hash)`,

		// Most recent first is how every listing reads.
		`CREATE INDEX IF NOT EXISTS ` + ModelTable + `_built_idx
			ON ` + ModelTable + ` (tenant_id, built_at DESC)`,

		`CREATE TABLE IF NOT EXISTS ` + BlobTable + ` (
			id            TEXT PRIMARY KEY,
			tenant_id     TEXT NOT NULL,
			version       BIGINT NOT NULL,
			content_hash  TEXT NOT NULL,
			body          BYTEA NOT NULL,
			size_bytes    INTEGER NOT NULL
		)`,

		`CREATE INDEX IF NOT EXISTS ` + BlobTable + `_tenant_idx
			ON ` + BlobTable + ` (tenant_id)`,

		// Unique here, unlike the model table: a blob *is* its content hash, so
		// two rows under one hash would be a contradiction rather than a
		// duplicate.
		`CREATE UNIQUE INDEX IF NOT EXISTS ` + BlobTable + `_hash_idx
			ON ` + BlobTable + ` (tenant_id, content_hash)`,

		// Row-level security, forced. Without FORCE, the table owner is exempt
		// and tenant isolation is silently a no-op for exactly the connection
		// most likely to be used by mistake.
		`ALTER TABLE ` + ModelTable + ` ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ` + ModelTable + ` FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_isolation ON ` + ModelTable,
		`CREATE POLICY tenant_isolation ON ` + ModelTable + `
			USING (tenant_id = current_setting('app.tenant_id', true))`,

		`ALTER TABLE ` + BlobTable + ` ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ` + BlobTable + ` FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_isolation ON ` + BlobTable,
		`CREATE POLICY tenant_isolation ON ` + BlobTable + `
			USING (tenant_id = current_setting('app.tenant_id', true))`,
	})
}

func down0001(ctx context.Context, exec migrate.Executor) error {
	// Dropped in reverse dependency order. Nothing references these, so the
	// order is only for symmetry with up — but a down migration that leaves a
	// table behind is one nobody trusts to have run.
	return execAll(ctx, exec, []string{
		`DROP TABLE IF EXISTS ` + BlobTable,
		`DROP TABLE IF EXISTS ` + ModelTable,
	})
}

// migration0003Identity adds branch identity and bus placements.
//
// A third migration rather than an edit to 0001 or 0002, for the same reason
// 0002 was: both have run in databases that exist, and grove records them as
// applied.
var migration0003Identity = &migrate.Migration{
	Name:    "networkmodel_id",
	Version: "20260805000010",
	Comment: "branch identity and bus placements, beside the canonical blob",
	Up:      up0003,
	Down:    down0003,
}

func up0003(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, []string{
		`CREATE TABLE IF NOT EXISTS ` + BranchTable + ` (
			id         TEXT PRIMARY KEY,
			tenant_id  TEXT NOT NULL,
			version    BIGINT NOT NULL,
			model_id   TEXT NOT NULL,
			ordinal    TEXT NOT NULL,
			from_bus   TEXT NOT NULL,
			to_bus     TEXT NOT NULL,
			alias      TEXT NOT NULL DEFAULT ''
		)`,

		// One branch per ordinal per model. Unique because two would be a
		// contradiction rather than a duplicate: nothing could say which
		// conductor a cascade event meant.
		`CREATE UNIQUE INDEX IF NOT EXISTS ` + BranchTable + `_ordinal_key
			ON ` + BranchTable + ` (tenant_id, model_id, ordinal)`,

		`CREATE TABLE IF NOT EXISTS ` + PlacementTable + ` (
			id         TEXT PRIMARY KEY,
			tenant_id  TEXT NOT NULL,
			version    BIGINT NOT NULL,
			model_id   TEXT NOT NULL,
			bus        TEXT NOT NULL,
			lon        DOUBLE PRECISION NOT NULL,
			lat        DOUBLE PRECISION NOT NULL,
			elevation  DOUBLE PRECISION NOT NULL DEFAULT 0
		)`,

		`CREATE UNIQUE INDEX IF NOT EXISTS ` + PlacementTable + `_bus_key
			ON ` + PlacementTable + ` (tenant_id, model_id, bus)`,

		// Row-level security, forced, and a tenant-isolation policy — the same
		// treatment up0001 gives ModelTable and BlobTable above. Without FORCE
		// the table owner is exempt and tenant isolation is silently a no-op
		// for exactly the connection most likely to be used by mistake.
		`ALTER TABLE ` + BranchTable + ` ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ` + BranchTable + ` FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_isolation ON ` + BranchTable,
		`CREATE POLICY tenant_isolation ON ` + BranchTable + `
			USING (tenant_id = current_setting('app.tenant_id', true))`,

		`ALTER TABLE ` + PlacementTable + ` ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ` + PlacementTable + ` FORCE ROW LEVEL SECURITY`,
		`DROP POLICY IF EXISTS tenant_isolation ON ` + PlacementTable,
		`CREATE POLICY tenant_isolation ON ` + PlacementTable + `
			USING (tenant_id = current_setting('app.tenant_id', true))`,
	})
}

func down0003(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, []string{
		`DROP TABLE IF EXISTS ` + PlacementTable,
		`DROP TABLE IF EXISTS ` + BranchTable,
	})
}

// execAll runs statements in order, naming the one that failed.
//
// A migration that reports only "syntax error" leaves the reader diffing a
// hundred lines of DDL against the database.
func execAll(ctx context.Context, exec migrate.Executor, statements []string) error {
	for index, statement := range statements {
		if _, err := exec.Exec(ctx, statement); err != nil {
			return fmt.Errorf("networkmodel: statement %d: %w", index+1, err)
		}
	}

	return nil
}

// migration0002BytePlane moves canonical encodings out of Postgres.
//
// A second migration rather than an edit to the first. 0001 has run in
// databases that exist, and grove records it as applied — editing it would
// leave every one of them without the change while reporting the group as up
// to date.
var migration0002BytePlane = &migrate.Migration{
	Name:    "networkmodel_byte_plane",
	Version: "20260802000030",
	Comment: "canonical encodings move to fabriq's content-addressable blob plane",
	Up:      up0002,
	Down:    down0002,
}

func up0002(ctx context.Context, exec migrate.Executor) error {
	return execAll(ctx, exec, []string{
		// The reference Fabriq's byte plane mints for the stored encoding.
		// Nullable in effect — defaulted empty — because models imported
		// before this migration have no blob there, and Encoding says so
		// rather than returning bytes that are not the ones it was hashed
		// from.
		`ALTER TABLE ` + ModelTable + `
			ADD COLUMN IF NOT EXISTS blob_ref TEXT NOT NULL DEFAULT ''`,

		// Dropped rather than left behind. A table holding a second copy of
		// every encoding, which nothing reads and no migration will ever
		// reconcile, is a copy that drifts — and the next person to find it
		// has no way to tell which copy was authoritative.
		`DROP TABLE IF EXISTS ` + BlobTable,
	})
}

func down0002(ctx context.Context, exec migrate.Executor) error {
	// The table comes back empty. Its contents were the encodings, and those
	// now live in the byte plane under references this column is about to
	// drop — so a down migration cannot restore them and should not pretend
	// to. Re-importing the source files rebuilds them, which is what the
	// content hash makes safe.
	return execAll(ctx, exec, []string{
		`ALTER TABLE ` + ModelTable + ` DROP COLUMN IF EXISTS blob_ref`,

		`CREATE TABLE IF NOT EXISTS ` + BlobTable + ` (
			id            TEXT PRIMARY KEY,
			tenant_id     TEXT NOT NULL,
			version       BIGINT NOT NULL,
			content_hash  TEXT NOT NULL,
			body          BYTEA NOT NULL,
			size_bytes    INTEGER NOT NULL
		)`,
	})
}
