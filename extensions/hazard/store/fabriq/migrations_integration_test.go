//go:build integration

package fabriq_test

import (
	"context"
	"testing"

	"github.com/xraph/grove/drivers/pgdriver"

	hzfabriq "github.com/xraph/blackstart/extensions/hazard/store/fabriq"
	"github.com/xraph/blackstart/internal/testsupport"
)

// Spec section 10 requires migrations to run forward and backward cleanly. The
// backward half is not ceremony here: the value table is converted to a
// hypertable inside a guarded DO block, and a plain DROP TABLE has to undo
// that as well as the ordinary tables.
func TestMigrationsRunBothWays(t *testing.T) {
	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, hzfabriq.Group())

	if got := hazardTableCount(t, superDSN); got == 0 {
		t.Fatal("migrating created no hazard tables")
	}

	testsupport.RollbackAll(t, superDSN, hzfabriq.Group())

	if got := hazardTableCount(t, superDSN); got != 0 {
		t.Errorf("%d hazard tables survived the rollback", got)
	}
}

// A second forward pass must succeed after a rollback, which is what makes the
// down step usable for anything other than a one-way experiment.
func TestMigrationsReapplyAfterRollback(t *testing.T) {
	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, hzfabriq.Group())

	testsupport.RollbackAll(t, superDSN, hzfabriq.Group())

	// Asserted before the second migrate, because Migrate only runs *pending*
	// migrations: a half-unwound group would skip re-applying the steps still
	// on record and the final count would look identical to a full round trip.
	if got := hazardTableCount(t, superDSN); got != 0 {
		t.Fatalf("%d hazard tables still present after the rollback, before the "+
			"second migrate — the rollback was only partial", got)
	}

	testsupport.Migrate(t, superDSN, hzfabriq.Group())

	if got := hazardTableCount(t, superDSN); got == 0 {
		t.Fatal("re-applying after a rollback created no hazard tables")
	}
}

// The value table has to be a hypertable where Timescale is available, not
// merely a table with the right columns. A DO block whose guard never matched
// would leave an ordinary table behind and every write would still succeed —
// slowly, and without any of the partitioning the series depends on.
func TestTheValueTableIsAHypertable(t *testing.T) {
	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, hzfabriq.Group())

	drv := pgdriver.New()
	if err := drv.Open(context.Background(), superDSN); err != nil {
		t.Fatalf("opening for inspection: %v", err)
	}

	defer func() { _ = drv.Close() }()

	var available []struct {
		N int `grove:"n"`
	}

	if err := drv.NewRaw(
		`SELECT count(*) AS n FROM pg_available_extensions WHERE name = 'timescaledb'`,
	).Scan(context.Background(), &available); err != nil {
		t.Fatalf("checking for timescaledb: %v", err)
	}

	if len(available) == 0 || available[0].N == 0 {
		t.Skip("no timescaledb in this environment; the guarded branch is inert")
	}

	var hypertables []struct {
		N int `grove:"n"`
	}

	if err := drv.NewRaw(
		`SELECT count(*) AS n FROM timescaledb_information.hypertables
		 WHERE hypertable_name = $1`, hzfabriq.ValueSeries,
	).Scan(context.Background(), &hypertables); err != nil {
		t.Fatalf("listing hypertables: %v", err)
	}

	if len(hypertables) == 0 || hypertables[0].N == 0 {
		t.Errorf("%s exists but is not a hypertable", hzfabriq.ValueSeries)
	}
}

func hazardTableCount(t *testing.T, dsn string) int {
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
		 WHERE table_name LIKE 'blackstart_hazard%'`,
	).Scan(context.Background(), &rows); err != nil {
		t.Fatalf("counting hazard tables: %v", err)
	}

	if len(rows) == 0 {
		t.Fatal("counting hazard tables returned no rows")
	}

	return rows[0].N
}
