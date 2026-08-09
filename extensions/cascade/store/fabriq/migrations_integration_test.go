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

	testsupport.RollbackAll(t, superDSN, cscfabriq.Group())

	if got := cascadeTableCount(t, superDSN); got != 0 {
		t.Errorf("%d cascade tables survived the rollback", got)
	}
}

// A second forward pass must succeed after a rollback, which is what makes the
// down step usable for anything other than a one-way experiment.
func TestMigrationsReapplyAfterRollback(t *testing.T) {
	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, cscfabriq.Group())

	testsupport.RollbackAll(t, superDSN, cscfabriq.Group())

	testsupport.Migrate(t, superDSN, cscfabriq.Group())

	if got := cascadeTableCount(t, superDSN); got == 0 {
		t.Fatal("re-applying after a rollback created no cascade tables")
	}

	// And the columns the later migrations add come back with it.
	for _, column := range []string{"initiating", "canonical_hash"} {
		if got := columnCount(t, superDSN, column); got != 1 {
			t.Errorf("the %s column did not survive a rollback and re-apply", column)
		}
	}
}

// The canonical hash column takes no default, so an existing row's answer stays
// unknown rather than becoming a hash every historical run shares.
//
// Asserted against the live schema rather than the DDL string, because the
// thing that matters is what Postgres ended up with: a NOT NULL default of the
// empty string, added later, would read perfectly well in review and would
// silently make every pre-existing run list as one network.
func TestTheCanonicalHashColumnIsNullableWithNoDefault(t *testing.T) {
	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, cscfabriq.Group())

	if got := scalar(t, superDSN,
		`SELECT count(*) AS n FROM information_schema.columns
		 WHERE table_name = 'blackstart_cascade_run'
		   AND column_name = 'canonical_hash'
		   AND is_nullable = 'YES'
		   AND column_default IS NULL`); got != 1 {
		t.Error("canonical_hash is not nullable-with-no-default; a run recorded " +
			"before this column has an unknown canonical hash, and any default " +
			"would claim every one of them belongs to the same network")
	}
}

func columnCount(t *testing.T, dsn, column string) int {
	t.Helper()

	return scalar(t, dsn,
		`SELECT count(*) AS n FROM information_schema.columns
		 WHERE table_name = 'blackstart_cascade_run' AND column_name = '`+column+`'`)
}

func cascadeTableCount(t *testing.T, dsn string) int {
	t.Helper()

	return scalar(t, dsn,
		`SELECT count(*) AS n FROM information_schema.tables
		 WHERE table_name LIKE 'blackstart_cascade%'`)
}

func scalar(t *testing.T, dsn, query string) int {
	t.Helper()

	drv := pgdriver.New()
	if err := drv.Open(context.Background(), dsn); err != nil {
		t.Fatalf("opening for inspection: %v", err)
	}

	defer func() { _ = drv.Close() }()

	var rows []struct {
		N int `grove:"n"`
	}

	if err := drv.NewRaw(query).Scan(context.Background(), &rows); err != nil {
		t.Fatalf("counting: %v", err)
	}

	if len(rows) == 0 {
		t.Fatal("the count returned no rows")
	}

	return rows[0].N
}
