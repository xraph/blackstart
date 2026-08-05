//go:build integration

package fabriq_test

import (
	"context"
	"testing"

	"github.com/xraph/grove/drivers/pgdriver"

	riskfabriq "github.com/xraph/blackstart/extensions/risk/store/fabriq"
	"github.com/xraph/blackstart/internal/testsupport"
)

// Spec section 10 requires migrations to run forward and backward cleanly. The
// backward half is not ceremony here: the value table is converted to a
// hypertable inside a guarded DO block, and a plain DROP TABLE has to undo
// that as well as the ordinary table.
func TestMigrationsRunBothWays(t *testing.T) {
	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, riskfabriq.Group())

	if got := riskTableCount(t, superDSN); got == 0 {
		t.Fatal("migrating created no risk tables")
	}

	rollbackRiskGroup(t, superDSN)

	if got := riskTableCount(t, superDSN); got != 0 {
		t.Errorf("%d risk tables survived the rollback", got)
	}
}

// A second forward pass must succeed after a rollback, which is what makes the
// down step usable for anything other than a one-way experiment.
func TestMigrationsReapplyAfterRollback(t *testing.T) {
	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, riskfabriq.Group())

	rollbackRiskGroup(t, superDSN)

	// A partial rollback wouldn't be caught by the re-migrate below — Migrate
	// only runs *pending* migrations, so a half-unwound group would just skip
	// re-applying the steps still on record and the final count would look
	// identical to a full round trip. Asserting zero here is what makes the
	// rest of the test mean what it claims.
	if got := riskTableCount(t, superDSN); got != 0 {
		t.Fatalf("%d risk tables still present after rollback, before the "+
			"second migrate — the rollback was only partial", got)
	}

	testsupport.Migrate(t, superDSN, riskfabriq.Group())

	if got := riskTableCount(t, superDSN); got == 0 {
		t.Fatal("re-applying after a rollback created no risk tables")
	}
}

// The value table has to be a hypertable where Timescale is available, not
// merely a table with the right columns. A DO block whose guard never matched
// would leave an ordinary table behind and every write would still succeed —
// slowly, and without any of the partitioning the series depends on.
func TestTheValueTableIsAHypertable(t *testing.T) {
	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, riskfabriq.Group())

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
		 WHERE hypertable_name = $1`, riskfabriq.ValueSeries,
	).Scan(context.Background(), &hypertables); err != nil {
		t.Fatalf("listing hypertables: %v", err)
	}

	if len(hypertables) == 0 || hypertables[0].N == 0 {
		t.Errorf("%s exists but is not a hypertable", riskfabriq.ValueSeries)
	}
}

// rollbackRiskGroup fully unwinds the risk migration group.
//
// testsupport.Rollback, per grove's orchestrator, undoes exactly one
// migration per call: the single most recently applied migration across
// every group handed to it. Because risk's migration runs after Fabriq's own
// (Group declares that dependency), risk's step is always the most recent
// history and is what a call unwinds first — but relying on that by counting
// calls "one, because there is one migration" is exactly the assumption that
// shipped broken in geo, where the group's migration count and the number of
// Rollback calls needed quietly diverged. Looping, bounded by the group's own
// migration count, keeps this correct even if a later migration is added to
// this group without anyone revisiting this helper.
//
// Bounded at the group's own migration count rather than looped until the
// count reaches zero: RemoveApplied runs every call regardless of whether a
// migration's Down statement actually did anything, so a silently-broken down
// step would otherwise never lower the count and the loop would spin forever
// waiting for a stop condition that can't fire. With a hard bound, that same
// broken step instead leaves risk tables behind after the last call, and the
// caller's own riskTableCount assertion catches it.
func rollbackRiskGroup(t *testing.T, dsn string) {
	t.Helper()

	steps := len(riskfabriq.Group().Migrations())

	for i := 0; i < steps; i++ {
		if err := testsupport.Rollback(t, dsn, riskfabriq.Group()); err != nil {
			t.Fatalf("rolling back (step %d of %d): %v", i+1, steps, err)
		}

		if riskTableCount(t, dsn) == 0 {
			return
		}
	}
}

func riskTableCount(t *testing.T, dsn string) int {
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
		 WHERE table_name LIKE 'blackstart_risk_%'`,
	).Scan(context.Background(), &rows); err != nil {
		t.Fatalf("counting risk tables: %v", err)
	}

	if len(rows) == 0 {
		t.Fatal("counting risk tables returned no rows")
	}

	return rows[0].N
}
