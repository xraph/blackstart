package bitemporal

import (
	"fmt"
	"strings"
)

// The SQL side of bitemporality.
//
// The predicate is generated in exactly one place. Hand-writing it per store
// is how the "OR valid_to IS NULL" variant, the closed-interval variant and
// the forgot-the-knowledge-axis variant all end up in the same codebase — each
// of which returns plausible rows, so none of them fails a test that was not
// written to catch it.

// Column names. These appear in migrations, models and queries, so they are
// declared once rather than spelled out at each site.
const (
	ColValidFrom     = "valid_from"
	ColValidTo       = "valid_to"
	ColKnowledgeFrom = "knowledge_from"
	ColKnowledgeTo   = "knowledge_to"

	// ColEntityID is the stable identifier of the thing being versioned, as
	// distinct from the row's own primary key. In the current table the two
	// coincide; in a history table many rows share one entity_id.
	ColEntityID = "entity_id"
)

// Columns returns the four interval column names, in declaration order.
func Columns() []string {
	return []string{ColValidFrom, ColValidTo, ColKnowledgeFrom, ColKnowledgeTo}
}

// Predicate renders the WHERE clause selecting the single version that answers
// a read, together with its arguments.
//
// firstArg is the 1-based index of the first placeholder, so the clause can be
// composed after other conditions: pass len(existingArgs)+1.
//
// The comparisons are half-open on both axes — from <= at AND at < to — which
// is what makes adjacent versions tile without a query at the boundary
// matching two of them.
func Predicate(alias string, a AsOf, firstArg int) (string, []any) {
	q := qualify(alias)

	clause := fmt.Sprintf(
		"%s%s <= $%d AND %s%s > $%d AND %s%s <= $%d AND %s%s > $%d",
		q, ColValidFrom, firstArg,
		q, ColValidTo, firstArg,
		q, ColKnowledgeFrom, firstArg+1,
		q, ColKnowledgeTo, firstArg+1,
	)

	return clause, []any{a.ValidAt.Time(), a.KnownAt.Time()}
}

// CurrentPredicate renders the WHERE clause selecting rows that are both
// currently believed and currently valid — the common "network as it is now"
// read.
//
// It is a query filter only. It cannot be a partial-index predicate, because
// now() is STABLE rather than IMMUTABLE and PostgreSQL rejects it there:
//
//	ERROR: functions in index predicate must be marked IMMUTABLE (SQLSTATE 42P17)
//
// Use CurrentIndexPredicate for the index. That distinction was found by the
// integration suite, not by reasoning — the first draft used this expression
// for both and failed at CREATE INDEX.
func CurrentPredicate(alias string) string {
	q := qualify(alias)

	return fmt.Sprintf(
		"%s AND %s%s > now()",
		CurrentIndexPredicate(alias),
		q, ColValidTo,
	)
}

// CurrentIndexPredicate renders the immutable half of CurrentPredicate, for
// use as a partial-index predicate.
//
// Only the knowledge axis is expressible here: "is this row still believed" is
// a comparison against a constant, while "is this row still valid" depends on
// the current time and therefore cannot be indexed.
//
// The narrower index is still worth having. Archived versions accumulate
// without bound as facts are corrected, while currently-believed rows stay
// proportional to the size of the network — so excluding history is where
// nearly all of the selectivity is. The remaining valid_to > now() check runs
// against that much smaller set.
func CurrentIndexPredicate(alias string) string {
	return fmt.Sprintf(
		"%s%s = '%s'",
		qualify(alias), ColKnowledgeTo, foreverLiteral,
	)
}

// foreverLiteral is the SQL spelling of Forever. It must match foreverTime
// exactly; a test asserts it does, because a mismatch would make
// CurrentPredicate silently match nothing.
const foreverLiteral = "9999-12-31 23:59:59.999999+00"

// ColumnDDL returns the column definitions for a bitemporal table, for
// interpolation into a CREATE TABLE.
//
// Every column is NOT NULL. Nullable ends are the footgun this package exists
// to avoid: they force an "OR ... IS NULL" branch into every predicate, defeat
// range scans, and make the exclusion constraint below inexpressible.
func ColumnDDL() string {
	return strings.Join([]string{
		ColValidFrom + "     TIMESTAMPTZ NOT NULL",
		ColValidTo + "       TIMESTAMPTZ NOT NULL",
		ColKnowledgeFrom + " TIMESTAMPTZ NOT NULL",
		ColKnowledgeTo + "   TIMESTAMPTZ NOT NULL",
	}, ",\n\t")
}

// IndexDDL returns the statements creating the indexes a bitemporal table
// needs. The table must already exist.
//
// Three indexes, each earning its place:
//
//   - A btree on (tenant, entity, knowledge_from DESC) serves "the history of
//     this entity", which is what an audit or explanation view asks for.
//   - A partial index on the current rows serves the common read — "the
//     network as it is now" — without a range scan. Most queries hit this.
//   - A GiST index over both ranges serves point-in-time reads, which are what
//     a backtest issues millions of.
func IndexDDL(table string) []string {
	return []string{
		fmt.Sprintf(
			`CREATE INDEX IF NOT EXISTS %s_history_idx ON %s (tenant_id, %s, %s DESC)`,
			table, table, ColEntityID, ColKnowledgeFrom),

		fmt.Sprintf(
			`CREATE INDEX IF NOT EXISTS %s_current_idx ON %s (tenant_id, %s) WHERE %s`,
			table, table, ColEntityID, CurrentIndexPredicate("")),

		fmt.Sprintf(
			`CREATE INDEX IF NOT EXISTS %s_asof_idx ON %s USING gist (
	tenant_id,
	%s,
	tstzrange(%s, %s),
	tstzrange(%s, %s)
)`,
			table, table, ColEntityID,
			ColValidFrom, ColValidTo,
			ColKnowledgeFrom, ColKnowledgeTo),
	}
}

// ExclusionDDL returns the constraint that makes "exactly one version answers
// any read" a property of the database rather than of the writing code.
//
// Without it, a bug in the supersession arithmetic produces overlapping
// versions, and every affected query silently returns whichever row the
// planner reached first — non-deterministically, and differently between
// development and production.
//
// Requires the btree_gist extension, since it mixes equality on scalar columns
// with overlap on ranges. The bootstrap migration creates it.
func ExclusionDDL(table string) string {
	return fmt.Sprintf(
		`ALTER TABLE %s ADD CONSTRAINT %s_no_overlap EXCLUDE USING gist (
	tenant_id WITH =,
	%s WITH =,
	tstzrange(%s, %s) WITH &&,
	tstzrange(%s, %s) WITH &&
)`,
		table, table, ColEntityID,
		ColValidFrom, ColValidTo,
		ColKnowledgeFrom, ColKnowledgeTo)
}

// RequiredExtensions lists the PostgreSQL extensions bitemporal tables depend
// on. The bootstrap migration creates them before any table is defined.
func RequiredExtensions() []string {
	return []string{"btree_gist"}
}

func qualify(alias string) string {
	if alias == "" {
		return ""
	}

	return alias + "."
}
