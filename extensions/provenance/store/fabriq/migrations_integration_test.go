//go:build integration

package fabriq_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/xraph/grove/drivers/pgdriver"

	pvfabriq "github.com/xraph/blackstart/extensions/provenance/store/fabriq"
	"github.com/xraph/blackstart/internal/testsupport"
)

// The single tenant every call in this package's suite is scoped to, and a
// sample hash. Declared here because this is the first file in the package
// that needs either; the store and graph suites share them.
const (
	tenantID = "blackstart"
	hash     = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

// A migration that cannot be rolled back is one nobody can test safely, which
// is why the project requires both directions to run cleanly. Migrating up
// again afterwards proves the down did not leave the schema in a state the up
// cannot start from — the failure mode a one-way test misses. It matters more
// than usual here: the down has to drop a shared function, not only tables.
func TestMigrationsRunBothWays(t *testing.T) {
	dsn := testsupport.Postgres(t)

	testsupport.Migrate(t, dsn, pvfabriq.Group())

	testsupport.RollbackAll(t, dsn, pvfabriq.Group())

	testsupport.Migrate(t, dsn, pvfabriq.Group())
}

// Append-only, enforced by the database rather than by the absence of a method.
//
// record.Store has no Update and no Delete, but that is a property of this
// codebase, not of the data. A psql session, a future store method or a
// well-meaning backfill script would all be able to amend a provenance record,
// and amending one is falsification rather than correction.
func TestTheDatabaseRefusesToAmendAProvenanceRecord(t *testing.T) {
	dsn := testsupport.Postgres(t)
	testsupport.Migrate(t, dsn, pvfabriq.Group())

	db := rawDB(t, dsn)
	ctx := context.Background()

	seedDataset(t, dsn, "dsv_amend", hash)

	_, err := db.NewRaw(
		`UPDATE `+pvfabriq.DatasetTable+` SET url = 'https://elsewhere.example' WHERE id = $1`,
		"dsv_amend").Exec(ctx)
	if err == nil {
		t.Fatal("the database accepted an UPDATE against a provenance record")
	}

	if !strings.Contains(err.Error(), "append-only") {
		t.Errorf("the refusal should explain itself, got: %v", err)
	}

	_, err = db.NewRaw(
		`DELETE FROM `+pvfabriq.DatasetTable+` WHERE id = $1`, "dsv_amend").Exec(ctx)
	if err == nil {
		t.Fatal("the database accepted a DELETE against a provenance record")
	}
}

// The dedupe path every ingest calls. Two retrievals with the same bytes are
// the same version whatever their timestamps say, and the index is the
// authority rather than a read-then-write check in the store, which two
// concurrent ingests would both pass.
func TestContentHashIsUniquePerTenant(t *testing.T) {
	dsn := testsupport.Postgres(t)
	testsupport.Migrate(t, dsn, pvfabriq.Group())

	seedDataset(t, dsn, "dsv_first", hash)

	if err := insertDataset(t, dsn, "dsv_second", hash); err == nil {
		t.Fatal("the database accepted two dataset versions with the same content hash")
	}
}

// A lineage edge pointing at a run that was never recorded is a dangling
// reference in the one structure whose whole value is that it can be followed.
// The constraint is composite over (tenant_id, id) because PostgreSQL runs
// foreign key checks as internal system triggers, which bypass row-level
// security — a simple REFERENCES parent(id) would let one tenant's edge
// reference another tenant's run.
func TestLineageCannotNameAnUnrecordedRun(t *testing.T) {
	dsn := testsupport.Postgres(t)
	testsupport.Migrate(t, dsn, pvfabriq.Group())

	db := rawDB(t, dsn)

	_, err := db.NewRaw(
		`INSERT INTO `+pvfabriq.LineageTable+`
		 (id, tenant_id, version, run_id, dataset_version_id, role, relation, recorded_at)
		 VALUES ($1, $2, 1, $3, $4, 'input', 'USED', $5)`,
		"lnge_orphan", tenantID, "run_absent", "dsv_absent", time.Now().UTC(),
	).Exec(context.Background())
	if err == nil {
		t.Fatal("the database accepted a lineage edge with no ends")
	}
}

// rawDB opens a driver handle for the raw SQL these tests issue.
//
// A driver rather than testsupport.GroveDB: NewRaw lives on the pgdriver, and
// the point of every test here is to reach the database directly, underneath
// the store — a guarantee the schema makes is not a guarantee if only the
// store's own code path respects it.
func rawDB(t *testing.T, dsn string) *pgdriver.PgDB {
	t.Helper()

	drv := pgdriver.New()
	if err := drv.Open(context.Background(), dsn); err != nil {
		t.Fatalf("opening %s: %v", dsn, err)
	}

	t.Cleanup(func() { _ = drv.Close() })

	return drv
}

// seedDataset inserts a dataset row directly, failing the test if it cannot.
func seedDataset(t *testing.T, dsn, datasetID, contentHash string) {
	t.Helper()

	if err := insertDataset(t, dsn, datasetID, contentHash); err != nil {
		t.Fatalf("seeding dataset %s: %v", datasetID, err)
	}
}

// insertDataset inserts a dataset row and returns the database's answer.
func insertDataset(t *testing.T, dsn, datasetID, contentHash string) error {
	t.Helper()

	_, err := rawDB(t, dsn).NewRaw(
		`INSERT INTO `+pvfabriq.DatasetTable+`
		 (id, tenant_id, version, source_id, url, content_hash, size_bytes,
		  parser_version, retrieved_at)
		 VALUES ($1, $2, 1, '', $3, $4, 0, $5, $6)`,
		datasetID, tenantID, "https://example.gov/data", contentHash,
		"test/0.1.0", time.Now().UTC(),
	).Exec(context.Background())

	return err
}
