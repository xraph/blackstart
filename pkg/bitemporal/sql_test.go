package bitemporal

import (
	"strings"
	"testing"
	"time"
)

// The SQL spelling of Forever must match the Go value exactly. If it drifts,
// CurrentPredicate matches nothing and every "as it is now" query quietly
// returns an empty result — which reads as "no data" rather than as a bug.
func TestForeverLiteralMatchesTheGoSentinel(t *testing.T) {
	t.Parallel()

	got := Forever().Time().UTC().Format("2006-01-02 15:04:05.999999-07")
	if got != foreverLiteral {
		t.Errorf("the SQL literal is %q but Forever() formats as %q; "+
			"CurrentPredicate would match nothing", foreverLiteral, got)
	}
}

// The predicate is half-open on both axes. A closed upper bound would make a
// query at a version boundary match two versions, and the exclusion constraint
// would not catch it because the rows themselves do not overlap.
func TestPredicateIsHalfOpenOnBothAxes(t *testing.T) {
	t.Parallel()

	clause, args := Predicate("t", Current(), 1)

	for _, want := range []string{
		"t.valid_from <= $1",
		"t.valid_to > $1",
		"t.knowledge_from <= $2",
		"t.knowledge_to > $2",
	} {
		if !strings.Contains(clause, want) {
			t.Errorf("predicate is missing %q:\n%s", want, clause)
		}
	}

	// A closed upper bound would spell these with >=.
	if strings.Contains(clause, "valid_to >= ") || strings.Contains(clause, "knowledge_to >= ") {
		t.Errorf("predicate uses a closed upper bound:\n%s", clause)
	}

	if len(args) != 2 {
		t.Fatalf("predicate returned %d args, want 2", len(args))
	}
}

func TestPredicateArgsAreValidThenKnown(t *testing.T) {
	t.Parallel()

	validAt := At(time.Date(2026, 2, 14, 3, 0, 0, 0, time.UTC))
	knownAt := At(time.Date(2026, 2, 13, 12, 0, 0, 0, time.UTC))

	_, args := Predicate("t", PointInTime(validAt, knownAt), 1)

	gotValid, ok := args[0].(time.Time)
	if !ok || !gotValid.Equal(validAt.Time()) {
		t.Errorf("first arg is %v, want the valid time %s", args[0], validAt)
	}

	gotKnown, ok := args[1].(time.Time)
	if !ok || !gotKnown.Equal(knownAt.Time()) {
		t.Errorf("second arg is %v, want the knowledge time %s", args[1], knownAt)
	}
}

// The clause composes after other conditions, so the placeholder numbering has
// to start where the caller says. Getting this wrong shifts every argument and
// produces a type error at best, a wrong answer at worst.
func TestPredicateHonorsTheArgumentOffset(t *testing.T) {
	t.Parallel()

	clause, _ := Predicate("t", Current(), 4)

	for _, want := range []string{"$4", "$5"} {
		if !strings.Contains(clause, want) {
			t.Errorf("offset predicate is missing %s:\n%s", want, clause)
		}
	}

	for _, unwanted := range []string{"$1", "$2", "$3"} {
		if strings.Contains(clause, unwanted) {
			t.Errorf("offset predicate should not reference %s:\n%s", unwanted, clause)
		}
	}
}

func TestPredicateWithoutAnAlias(t *testing.T) {
	t.Parallel()

	clause, _ := Predicate("", Current(), 1)

	if strings.Contains(clause, ".") {
		t.Errorf("unaliased predicate should not qualify columns:\n%s", clause)
	}

	if !strings.Contains(clause, "valid_from <= $1") {
		t.Errorf("unaliased predicate is malformed:\n%s", clause)
	}
}

func TestCurrentPredicateConstrainsBothAxes(t *testing.T) {
	t.Parallel()

	clause := CurrentPredicate("t")

	if strings.Contains(clause, "$") {
		t.Errorf("CurrentPredicate should be parameter-free:\n%s", clause)
	}

	if !strings.Contains(clause, ColKnowledgeTo) || !strings.Contains(clause, ColValidTo) {
		t.Errorf("CurrentPredicate must constrain both axes:\n%s", clause)
	}
}

// A partial-index predicate must be IMMUTABLE. now() is STABLE, and
// PostgreSQL rejects it outright:
//
//	ERROR: functions in index predicate must be marked IMMUTABLE
//
// Parameter-free is not sufficient — the first draft of this package checked
// only that, used one expression for both purposes, and failed at CREATE INDEX
// in the integration suite.
func TestCurrentIndexPredicateIsImmutable(t *testing.T) {
	t.Parallel()

	clause := CurrentIndexPredicate("t")

	if strings.Contains(clause, "now()") {
		t.Errorf("the index predicate calls now(), which is STABLE not "+
			"IMMUTABLE; CREATE INDEX will reject it:\n%s", clause)
	}

	if strings.Contains(clause, "$") {
		t.Errorf("the index predicate must be parameter-free:\n%s", clause)
	}

	if !strings.Contains(clause, ColKnowledgeTo) {
		t.Errorf("the index predicate must constrain the knowledge axis:\n%s", clause)
	}
}

// The generated index DDL must use the immutable form, not the query form.
func TestIndexDDLUsesTheImmutablePredicate(t *testing.T) {
	t.Parallel()

	for _, stmt := range IndexDDL("blackstart_bus") {
		if strings.Contains(stmt, "now()") {
			t.Errorf("index DDL calls now(), which PostgreSQL rejects in an "+
				"index predicate:\n%s", stmt)
		}
	}
}

// Nullable end times are the footgun this package avoids. If a column ever
// loses NOT NULL, every predicate needs an "OR ... IS NULL" branch and the
// exclusion constraint stops being expressible.
func TestEveryIntervalColumnIsNotNull(t *testing.T) {
	t.Parallel()

	ddl := ColumnDDL()

	for _, col := range Columns() {
		if !strings.Contains(ddl, col) {
			t.Errorf("column DDL is missing %s:\n%s", col, ddl)
		}
	}

	if got := strings.Count(ddl, "NOT NULL"); got != len(Columns()) {
		t.Errorf("only %d of %d interval columns are NOT NULL:\n%s",
			got, len(Columns()), ddl)
	}

	if strings.Count(ddl, "TIMESTAMPTZ") != len(Columns()) {
		t.Errorf("interval columns must all be TIMESTAMPTZ:\n%s", ddl)
	}
}

func TestIndexDDLCoversTheThreeAccessPatterns(t *testing.T) {
	t.Parallel()

	stmts := IndexDDL("blackstart_bus")
	if len(stmts) != 3 {
		t.Fatalf("expected 3 index statements, got %d", len(stmts))
	}

	all := strings.Join(stmts, "\n")

	for _, want := range []string{
		"blackstart_bus_history_idx",
		"blackstart_bus_current_idx",
		"blackstart_bus_asof_idx",
		"USING gist",
		"tstzrange",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("index DDL is missing %q:\n%s", want, all)
		}
	}

	// The current-row index must be partial, or it indexes every historical
	// version and stops being the cheap path it exists to be.
	if !strings.Contains(stmts[1], " WHERE ") {
		t.Errorf("the current-row index is not partial:\n%s", stmts[1])
	}
}

// The exclusion constraint is what makes "exactly one version answers" a
// property of the database rather than of the writing code.
func TestExclusionDDLConstrainsBothAxes(t *testing.T) {
	t.Parallel()

	ddl := ExclusionDDL("blackstart_bus")

	for _, want := range []string{
		"EXCLUDE USING gist",
		"tenant_id WITH =",
		ColEntityID + " WITH =",
		"tstzrange(" + ColValidFrom + ", " + ColValidTo + ") WITH &&",
		"tstzrange(" + ColKnowledgeFrom + ", " + ColKnowledgeTo + ") WITH &&",
	} {
		if !strings.Contains(ddl, want) {
			t.Errorf("exclusion constraint is missing %q:\n%s", want, ddl)
		}
	}
}

func TestRequiredExtensionsIncludesBtreeGist(t *testing.T) {
	t.Parallel()

	// The exclusion constraint mixes equality on scalars with overlap on
	// ranges, which plain GiST cannot do.
	var found bool

	for _, ext := range RequiredExtensions() {
		if ext == "btree_gist" {
			found = true
		}
	}

	if !found {
		t.Error("btree_gist is required for the exclusion constraint but is " +
			"not listed as a required extension")
	}
}
