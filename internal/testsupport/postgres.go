//go:build integration

package testsupport

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/fabriq/fabriqtest"
	fabriqmigrations "github.com/xraph/fabriq/migrations"
	"github.com/xraph/grove"
	"github.com/xraph/grove/drivers/pgdriver"
	_ "github.com/xraph/grove/drivers/pgdriver/pgmigrate"
	"github.com/xraph/grove/migrate"

	"github.com/xraph/blackstart/pkg/bitemporal"
)

// The containerized database harness, behind the `integration` build tag:
//
//	go test -tags=integration ./...
//	make test-integration
//
// It builds on fabriqtest rather than starting its own containers. That is
// deliberate — fabriqtest already pins the image blackstart needs
// (timescale/timescaledb-ha:pg16-all, which bundles PostGIS and pgvector) and
// already solves the part that is easy to get wrong: creating a non-superuser
// application role. PostgreSQL row-level security never constrains a
// superuser, so a harness that connects as one would pass every tenant
// isolation test while proving nothing.
//
// Containers are started per test and terminate with it. That is slower than
// a shared fixture and much easier to reason about, and it is the reason the
// integration suite is a separate target rather than part of `make test`.

// Postgres starts a PostgreSQL container with the extensions blackstart
// needs and returns the superuser DSN.
//
// btree_gist is not optional: the bitemporal exclusion constraint mixes
// equality on scalar columns with overlap on ranges, which plain GiST cannot
// index. postgis carries the network geometry.
func Postgres(t testing.TB) string {
	t.Helper()

	dsn := fabriqtest.StartPostgres(t)

	stmts := []string{`CREATE EXTENSION IF NOT EXISTS postgis`}
	for _, ext := range bitemporal.RequiredExtensions() {
		stmts = append(stmts, `CREATE EXTENSION IF NOT EXISTS `+ext)
	}

	fabriqtest.ApplyDDL(t, dsn, stmts)

	return dsn
}

// AppDSN provisions the restricted role the application connects as, and
// returns its DSN.
//
// Call it after migrations: the role is granted on existing tables, and the
// default privileges it sets apply only to tables created afterwards.
func AppDSN(t testing.TB, superDSN string) string {
	t.Helper()

	return fabriqtest.CreateAppRole(t, superDSN)
}

// GroveDB opens a grove handle on a DSN and closes it with the test.
func GroveDB(t testing.TB, dsn string) *grove.DB {
	t.Helper()

	drv := pgdriver.New()
	if err := drv.Open(context.Background(), dsn); err != nil {
		t.Fatalf("testsupport: opening %s: %v", redact(dsn), err)
	}

	gdb, err := grove.Open(drv)
	if err != nil {
		t.Fatalf("testsupport: wrapping the driver: %v", err)
	}

	t.Cleanup(func() {
		if err := gdb.Close(); err != nil {
			t.Logf("testsupport: closing the database: %v", err)
		}
	})

	return gdb
}

// ApplyDDL runs statements against a DSN, failing the test on error.
func ApplyDDL(t testing.TB, dsn string, stmts []string) {
	t.Helper()

	fabriqtest.ApplyDDL(t, dsn, stmts)
}

// redact removes the password from a DSN before it reaches a log line.
func redact(dsn string) string {
	at := strings.LastIndex(dsn, "@")
	scheme := strings.Index(dsn, "://")

	if at < 0 || scheme < 0 {
		return dsn
	}

	return dsn[:scheme+3] + "***" + dsn[at:]
}

// Migrate runs Fabriq's migration group together with the given groups,
// through a single orchestrator.
//
// One orchestrator is the point. Grove resolves migrate.DependsOn
// topologically only when every group is handed to the same one; running each
// extension's separately ignores the declaration silently, which grove's own
// source notes. Since every blackstart group depends on Fabriq's — its outbox
// and projection-state tables underpin every write — getting this wrong would
// produce tables in the wrong order and a failure that reads like a bad
// migration rather than a bad harness.
func Migrate(t testing.TB, dsn string, groups ...*migrate.Group) {
	t.Helper()

	drv := pgdriver.New()
	if err := drv.Open(context.Background(), dsn); err != nil {
		t.Fatalf("testsupport: opening %s for migrations: %v", redact(dsn), err)
	}

	defer func() { _ = drv.Close() }()

	exec, err := migrate.NewExecutorFor(drv)
	if err != nil {
		t.Fatalf("testsupport: building the migration executor: %v", err)
	}

	all := append([]*migrate.Group{fabriqmigrations.Group()}, groups...)

	if _, err := migrate.NewOrchestrator(exec, all...).Migrate(context.Background()); err != nil {
		t.Fatalf("testsupport: migrating: %v", err)
	}
}

// Rollback reverses the given groups together with Fabriq's, through one
// orchestrator. Used by the tests that assert migrations run both ways.
func Rollback(t testing.TB, dsn string, groups ...*migrate.Group) error {
	t.Helper()

	drv := pgdriver.New()
	if err := drv.Open(context.Background(), dsn); err != nil {
		return err
	}

	defer func() { _ = drv.Close() }()

	exec, err := migrate.NewExecutorFor(drv)
	if err != nil {
		return err
	}

	all := append([]*migrate.Group{fabriqmigrations.Group()}, groups...)

	_, err = migrate.NewOrchestrator(exec, all...).Rollback(context.Background())

	return err
}
