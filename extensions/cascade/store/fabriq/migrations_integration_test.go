//go:build integration

package fabriq_test

import (
	"context"
	"testing"

	"github.com/xraph/grove/drivers/pgdriver"

	cscfabriq "github.com/xraph/blackstart/extensions/cascade/store/fabriq"
	"github.com/xraph/blackstart/internal/testsupport"
)

// Spec section 10 requires migrations to run forward and backward cleanly.
func TestMigrationsRunBothWays(t *testing.T) {
	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, cscfabriq.Group())

	if got := cascadeTableCount(t, superDSN); got == 0 {
		t.Fatal("migrating created no cascade tables")
	}

	if err := testsupport.Rollback(t, superDSN, cscfabriq.Group()); err != nil {
		t.Fatalf("rolling back: %v", err)
	}

	if got := cascadeTableCount(t, superDSN); got != 0 {
		t.Errorf("%d cascade tables survived the rollback", got)
	}
}

// A second forward pass must succeed after a rollback, which is what makes the
// down step usable for anything other than a one-way experiment.
func TestMigrationsReapplyAfterRollback(t *testing.T) {
	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, cscfabriq.Group())

	if err := testsupport.Rollback(t, superDSN, cscfabriq.Group()); err != nil {
		t.Fatalf("rolling back: %v", err)
	}

	testsupport.Migrate(t, superDSN, cscfabriq.Group())

	if got := cascadeTableCount(t, superDSN); got == 0 {
		t.Fatal("re-applying after a rollback created no cascade tables")
	}
}

func cascadeTableCount(t *testing.T, dsn string) int {
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
		 WHERE table_name LIKE 'blackstart_cascade%'`,
	).Scan(context.Background(), &rows); err != nil {
		t.Fatalf("counting cascade tables: %v", err)
	}

	if len(rows) == 0 {
		t.Fatal("counting cascade tables returned no rows")
	}

	return rows[0].N
}
