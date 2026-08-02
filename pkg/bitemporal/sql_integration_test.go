//go:build integration

package bitemporal_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/xraph/grove/drivers/pgdriver"

	"github.com/xraph/blackstart/internal/testsupport"
	"github.com/xraph/blackstart/pkg/bitemporal"
	"github.com/xraph/blackstart/pkg/store/fabriqstore"
)

// These tests close the gap the unit suite cannot: Record.Matches is the Go
// spelling of the bitemporal rule and Predicate is the SQL spelling, and
// nothing so far has proved they agree. A divergence between them does not
// fail — it returns different rows in Go than in the database, which is the
// hardest kind of bug to see.

const table = "bitemporal_probe"

func setup(t *testing.T) (string, *pgdriver.PgDB) {
	t.Helper()

	dsn := testsupport.Postgres(t)

	ddl := []string{
		`CREATE TABLE ` + table + ` (
			id            TEXT PRIMARY KEY,
			tenant_id     TEXT NOT NULL,
			` + bitemporal.ColEntityID + ` TEXT NOT NULL,
			label         TEXT NOT NULL,
			` + bitemporal.ColumnDDL() + `
		)`,
	}
	ddl = append(ddl, bitemporal.IndexDDL(table)...)
	ddl = append(ddl, bitemporal.ExclusionDDL(table))

	testsupport.ApplyDDL(t, dsn, ddl)

	gdb := testsupport.GroveDB(t, dsn)

	pg, err := fabriqstore.PgOrErr(gdb)
	if err != nil {
		t.Fatalf("driver: %v", err)
	}

	return dsn, pg
}

func at(day int) bitemporal.Instant {
	return bitemporal.At(time.Date(2026, 2, day, 0, 0, 0, 0, time.UTC))
}

func insert(t *testing.T, pg *pgdriver.PgDB, id, entity, label string, r bitemporal.Record) error {
	t.Helper()

	_, err := pg.Exec(context.Background(),
		`INSERT INTO `+table+` (id, tenant_id, `+bitemporal.ColEntityID+`, label,
			valid_from, valid_to, knowledge_from, knowledge_to)
		 VALUES ($1, 'blackstart', $2, $3, $4, $5, $6, $7)`,
		id, entity, label,
		r.Valid.From.Time(), r.Valid.To.Time(),
		r.Knowledge.From.Time(), r.Knowledge.To.Time())

	return err
}

// The headline integration test. For every combination of versions and read
// points, the database must return exactly the rows Record.Matches selects.
func TestSQLPredicateAgreesWithRecordMatches(t *testing.T) {
	_, pg := setup(t)

	// The same history the unit suite uses: a fact opened on the 1st,
	// corrected on the 5th, then ended on the 10th as learned on the 12th.
	v1, err := bitemporal.Open(at(1), at(1))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	archivedV1, corrected, err := bitemporal.Correct(v1, at(5))
	if err != nil {
		t.Fatalf("Correct: %v", err)
	}

	archivedV2, ended, err := bitemporal.Close(corrected, at(10), at(12))
	if err != nil {
		t.Fatalf("Close: %v", err)
	}

	versions := map[string]bitemporal.Record{
		"v1": archivedV1,
		"v2": archivedV2,
		"v3": ended,
	}

	for label, rec := range versions {
		if err := insert(t, pg, label, "probe-1", label, rec); err != nil {
			t.Fatalf("inserting %s (%s): %v", label, rec, err)
		}
	}

	for _, validDay := range []int{1, 3, 6, 9, 10, 11, 15} {
		for _, knownDay := range []int{1, 3, 6, 9, 11, 13, 20} {
			asOf := bitemporal.PointInTime(at(validDay), at(knownDay))

			var wantLabels []string

			for label, rec := range versions {
				if rec.Matches(asOf) {
					wantLabels = append(wantLabels, label)
				}
			}

			gotLabels := queryLabels(t, pg, asOf)

			if !sameSet(wantLabels, gotLabels) {
				t.Errorf("%s: Go selected %v, SQL selected %v — the two "+
					"spellings of the bitemporal rule disagree",
					asOf, sorted(wantLabels), sorted(gotLabels))
			}
		}
	}
}

func queryLabels(t *testing.T, pg *pgdriver.PgDB, asOf bitemporal.AsOf) []string {
	t.Helper()

	pred, args := bitemporal.Predicate("p", asOf, 1)

	rows, err := pg.Query(context.Background(),
		`SELECT label FROM `+table+` AS p WHERE `+pred+` ORDER BY label`, args...)
	if err != nil {
		t.Fatalf("querying at %s: %v", asOf, err)
	}
	defer func() { _ = rows.Close() }()

	var out []string

	for rows.Next() {
		var label string
		if err := rows.Scan(&label); err != nil {
			t.Fatalf("scanning: %v", err)
		}

		out = append(out, label)
	}

	return out
}

// The exclusion constraint is what makes "exactly one version answers" a
// property of the database rather than of the writing code. If it does not
// actually reject an overlap, a bug in the supersession arithmetic produces
// rows the planner picks between non-deterministically.
func TestExclusionConstraintRejectsOverlappingVersions(t *testing.T) {
	_, pg := setup(t)

	base, err := bitemporal.Open(at(1), at(1))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := insert(t, pg, "first", "probe-2", "first", base); err != nil {
		t.Fatalf("inserting the first version: %v", err)
	}

	// A second open-ended version of the same entity overlaps on both axes.
	if err := insert(t, pg, "second", "probe-2", "second", base); err == nil {
		t.Fatal("the database accepted two overlapping versions of one entity; " +
			"a query would then return whichever row the planner reached first")
	}

	// A different entity with identical intervals is fine — the constraint is
	// scoped per entity, not global.
	if err := insert(t, pg, "other", "probe-3", "other", base); err != nil {
		t.Errorf("the constraint rejected a different entity: %v", err)
	}
}

// Adjacent versions must tile: the half-open interval means a fact ending at
// the 10th and its successor starting at the 10th do not overlap.
func TestAdjacentVersionsAreAccepted(t *testing.T) {
	_, pg := setup(t)

	first, err := bitemporal.Open(at(1), at(1))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	_, closed, err := bitemporal.Close(first, at(10), at(12))
	if err != nil {
		t.Fatalf("Close: %v", err)
	}

	successor, err := bitemporal.Open(at(10), at(12))
	if err != nil {
		t.Fatalf("Open successor: %v", err)
	}

	if err := insert(t, pg, "closed", "probe-4", "closed", closed); err != nil {
		t.Fatalf("inserting the closed version: %v", err)
	}

	if err := insert(t, pg, "successor", "probe-4", "successor", successor); err != nil {
		t.Fatalf("the constraint rejected an adjacent version; half-open "+
			"intervals should tile exactly: %v", err)
	}
}

func sorted(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)

	return out
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	seen := make(map[string]int, len(a))
	for _, v := range a {
		seen[v]++
	}

	for _, v := range b {
		seen[v]--
		if seen[v] < 0 {
			return false
		}
	}

	return true
}
