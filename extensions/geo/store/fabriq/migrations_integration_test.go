//go:build integration

package fabriq_test

import (
	"context"
	"testing"

	"github.com/xraph/grove/drivers/pgdriver"

	geofabriq "github.com/xraph/blackstart/extensions/geo/store/fabriq"
	"github.com/xraph/blackstart/internal/testsupport"
)

// Spec section 10 requires migrations to run forward and backward cleanly.
// This covers 0001-0003 along with the new 0004: none of the earlier down
// steps had a test before this one, and 0002 renames a column while 0003
// creates a table — either kind of change is easy to get backward wrong in a
// way that never shows up going forward.
func TestMigrationsRunBothWays(t *testing.T) {
	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, geofabriq.Group())

	if got := geoTableCount(t, superDSN); got == 0 {
		t.Fatal("migrating created no geo tables")
	}

	if err := testsupport.Rollback(t, superDSN, geofabriq.Group()); err != nil {
		t.Fatalf("rolling back: %v", err)
	}

	if got := geoTableCount(t, superDSN); got != 0 {
		t.Errorf("%d geo tables survived the rollback", got)
	}
}

// A second forward pass must succeed after a rollback, which is what makes the
// down step usable for anything other than a one-way experiment.
func TestMigrationsReapplyAfterRollback(t *testing.T) {
	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, geofabriq.Group())

	if err := testsupport.Rollback(t, superDSN, geofabriq.Group()); err != nil {
		t.Fatalf("rolling back: %v", err)
	}

	testsupport.Migrate(t, superDSN, geofabriq.Group())

	if got := geoTableCount(t, superDSN); got == 0 {
		t.Fatal("re-applying after a rollback created no geo tables")
	}
}

func geoTableCount(t *testing.T, dsn string) int {
	t.Helper()

	drv := pgdriver.New()
	if err := drv.Open(context.Background(), dsn); err != nil {
		t.Fatalf("opening for inspection: %v", err)
	}

	defer func() { _ = drv.Close() }()

	var rows []struct {
		N int `grove:"n"`
	}

	if err := drv.NewRaw(
		`SELECT count(*) AS n FROM information_schema.tables
		 WHERE table_name LIKE 'blackstart_geo_%'`,
	).Scan(context.Background(), &rows); err != nil {
		t.Fatalf("counting geo tables: %v", err)
	}

	if len(rows) == 0 {
		t.Fatal("counting geo tables returned no rows")
	}

	return rows[0].N
}
