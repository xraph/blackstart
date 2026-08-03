//go:build integration

package fabriq_test

import (
	"context"
	"testing"

	"github.com/xraph/grove/drivers/pgdriver"

	dsfabriq "github.com/xraph/blackstart/extensions/datasource/store/fabriq"
	wxfabriq "github.com/xraph/blackstart/extensions/weather/store/fabriq"
	"github.com/xraph/blackstart/internal/testsupport"
)

// Spec section 10 requires migrations to run forward and backward cleanly. The
// backward half is not ceremony here: the series tables are converted to
// hypertables inside a guarded DO block, and a plain DROP TABLE has to undo
// that as well as the ordinary tables.
func TestMigrationsRunBothWays(t *testing.T) {
	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, dsfabriq.Group(), wxfabriq.Group())

	if got := weatherTableCount(t, superDSN); got == 0 {
		t.Fatal("migrating created no weather tables")
	}

	if err := testsupport.Rollback(t, superDSN, dsfabriq.Group(), wxfabriq.Group()); err != nil {
		t.Fatalf("rolling back: %v", err)
	}

	if got := weatherTableCount(t, superDSN); got != 0 {
		t.Errorf("%d weather tables survived the rollback", got)
	}
}

// A second forward pass must succeed after a rollback, which is what makes the
// down step usable for anything other than a one-way experiment.
func TestMigrationsReapplyAfterRollback(t *testing.T) {
	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, dsfabriq.Group(), wxfabriq.Group())

	if err := testsupport.Rollback(t, superDSN, dsfabriq.Group(), wxfabriq.Group()); err != nil {
		t.Fatalf("rolling back: %v", err)
	}

	testsupport.Migrate(t, superDSN, dsfabriq.Group(), wxfabriq.Group())

	if got := weatherTableCount(t, superDSN); got == 0 {
		t.Fatal("re-applying after a rollback created no weather tables")
	}
}

func weatherTableCount(t *testing.T, dsn string) int {
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
		 WHERE table_name LIKE 'blackstart_weather%'`,
	).Scan(context.Background(), &rows); err != nil {
		t.Fatalf("counting weather tables: %v", err)
	}

	if len(rows) == 0 {
		t.Fatal("counting weather tables returned no rows")
	}

	return rows[0].N
}
