//go:build integration

package fabriq_test

import (
	"context"
	"testing"

	"github.com/xraph/grove/drivers/pgdriver"

	scnfabriq "github.com/xraph/blackstart/extensions/scenario/store/fabriq"
	"github.com/xraph/blackstart/internal/testsupport"
)

// registered is how many migrations this group carries.
//
// Grove rolls back one migration per call rather than a whole batch, so a
// group with more than one needs that many calls to unwind.
const registered = 1

// Spec section 10 requires migrations to run forward and backward cleanly.
func TestMigrationsRunBothWays(t *testing.T) {
	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, scnfabriq.Group())

	if got := scenarioTableCount(t, superDSN); got == 0 {
		t.Fatal("migrating created no scenario tables")
	}

	rollBackFully(t, superDSN)

	if got := scenarioTableCount(t, superDSN); got != 0 {
		t.Errorf("%d scenario tables survived the rollback", got)
	}
}

func rollBackFully(t *testing.T, dsn string) {
	t.Helper()

	for step := range registered {
		if err := testsupport.Rollback(t, dsn, scnfabriq.Group()); err != nil {
			t.Fatalf("rolling back step %d: %v", step+1, err)
		}
	}
}

// A second forward pass must succeed after a rollback, which is what makes
// the down step usable for anything other than a one-way experiment.
func TestMigrationsReapplyAfterRollback(t *testing.T) {
	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, scnfabriq.Group())

	rollBackFully(t, superDSN)

	testsupport.Migrate(t, superDSN, scnfabriq.Group())

	if got := scenarioTableCount(t, superDSN); got != 2 {
		t.Errorf("re-applying after a rollback created %d scenario tables, want 2", got)
	}
}

func scenarioTableCount(t *testing.T, dsn string) int {
	t.Helper()

	return scalar(t, dsn,
		`SELECT count(*) AS n FROM information_schema.tables
		 WHERE table_name LIKE 'blackstart_scenario%'`)
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
