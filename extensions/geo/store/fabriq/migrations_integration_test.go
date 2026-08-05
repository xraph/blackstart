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

	rollbackGeoGroup(t, superDSN)

	if got := geoTableCount(t, superDSN); got != 0 {
		t.Errorf("%d geo tables survived the rollback", got)
	}
}

// A second forward pass must succeed after a rollback, which is what makes the
// down step usable for anything other than a one-way experiment.
func TestMigrationsReapplyAfterRollback(t *testing.T) {
	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, geofabriq.Group())

	rollbackGeoGroup(t, superDSN)

	// A partial rollback wouldn't be caught by the re-migrate below — Migrate
	// only runs *pending* migrations, so a half-unwound group would just skip
	// re-applying the steps still on record and the final count would look
	// identical to a full round trip. Asserting zero here is what makes the
	// rest of the test mean what it claims.
	if got := geoTableCount(t, superDSN); got != 0 {
		t.Fatalf("%d geo tables still present after rollback, before the "+
			"second migrate — the rollback was only partial", got)
	}

	testsupport.Migrate(t, superDSN, geofabriq.Group())

	if got := geoTableCount(t, superDSN); got == 0 {
		t.Fatal("re-applying after a rollback created no geo tables")
	}
}

// rollbackGeoGroup fully unwinds the geo migration group.
//
// testsupport.Rollback, per grove's orchestrator, undoes exactly one
// migration per call: the single most recently applied migration across
// every group handed to it. Because geo's migrations run after Fabriq's own
// (Group declares that dependency), geo's steps are always the most recent
// history and are what a call unwinds first — so calling Rollback once per
// registered geo migration, most-recently-applied first, is what it takes to
// reach zero geo tables, not one call as the earlier draft assumed.
//
// Bounded at the group's own migration count rather than looped until the
// count reaches zero: RemoveApplied runs every call regardless of whether a
// migration's Down statement actually did anything, so a silently-broken
// down step would otherwise never lower the count and the loop would spin
// forever waiting for a stop condition that can't fire. With a hard bound,
// that same broken step instead leaves geo tables behind after the last call,
// and the caller's own geoTableCount assertion catches it.
func rollbackGeoGroup(t *testing.T, dsn string) {
	t.Helper()

	steps := len(geofabriq.Group().Migrations())

	for i := 0; i < steps; i++ {
		if err := testsupport.Rollback(t, dsn, geofabriq.Group()); err != nil {
			t.Fatalf("rolling back (step %d of %d): %v", i+1, steps, err)
		}

		if geoTableCount(t, dsn) == 0 {
			return
		}
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
