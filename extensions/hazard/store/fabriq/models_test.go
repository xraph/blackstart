package fabriq

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xraph/blackstart/extensions/hazard/conductor"
	hid "github.com/xraph/blackstart/extensions/hazard/id"
	"github.com/xraph/blackstart/extensions/hazard/store"
	"github.com/xraph/blackstart/pkg/id"
)

// The composite has to satisfy the interface. A compile-time assertion rather
// than a runtime one, so a method dropped in a refactor fails the build.
var _ store.Store = (*Store)(nil)

// An assignment survives the round trip through its persistence shape.
//
// The mappers are hand-written in both directions, which is what makes this
// worth testing: a field added to the domain type and forgotten in one mapper
// compiles, stores, and reads back as a zero.
func TestAssignmentModelRoundTrips(t *testing.T) {
	t.Parallel()

	want := conductor.Assignment{
		ID:            hid.NewAssignment(),
		CarrierID:     id.MustNew("brch"),
		ConductorCode: "cardinal",
		Basis:         conductor.AssumedByVoltage,
		Note:          "345 kV, no survey on file",
	}

	got, err := assignmentFromModel(*assignmentToModel(want))
	if err != nil {
		t.Fatalf("assignmentFromModel: %v", err)
	}

	if got != want {
		t.Errorf("round trip gave %+v, want %+v", got, want)
	}
}

// An assignment with no note round-trips too. The column defaults to the empty
// string and the field carries no notnull tag for exactly this case — fabriq
// refuses an empty value for a notnull field, which the grid table taught us
// the expensive way.
func TestAssignmentWithoutANoteRoundTrips(t *testing.T) {
	t.Parallel()

	want := conductor.Assignment{
		ID:            hid.NewAssignment(),
		CarrierID:     id.MustNew("brch"),
		ConductorCode: "drake",
		Basis:         conductor.Surveyed,
	}

	got, err := assignmentFromModel(*assignmentToModel(want))
	if err != nil {
		t.Fatalf("assignmentFromModel: %v", err)
	}

	if got != want {
		t.Errorf("round trip gave %+v, want %+v", got, want)
	}
}

func TestComputationModelRoundTrips(t *testing.T) {
	t.Parallel()

	want := store.Computation{
		ID:         hid.NewComputation(),
		CycleID:    id.MustNew("wxcyc"),
		RunID:      id.MustNew("prvrun"),
		ComputedAt: time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC),
		Spans:      1204,
		Unrated:    17,
		Values:     6020,
	}

	got, err := computationFromModel(*computationToModel(want))
	if err != nil {
		t.Fatalf("computationFromModel: %v", err)
	}

	if got != want {
		t.Errorf("round trip gave %+v, want %+v", got, want)
	}
}

// A pass recorded without a provenance recorder has no run, and the zero
// identifier has to survive as absence rather than come back as a parse error
// on the empty string.
func TestComputationWithoutARunRoundTrips(t *testing.T) {
	t.Parallel()

	want := store.Computation{
		ID:         hid.NewComputation(),
		CycleID:    id.MustNew("wxcyc"),
		ComputedAt: time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC),
	}

	got, err := computationFromModel(*computationToModel(want))
	if err != nil {
		t.Fatalf("computationFromModel: %v", err)
	}

	if !got.RunID.IsZero() {
		t.Errorf("a missing run came back as %s", got.RunID)
	}

	if got != want {
		t.Errorf("round trip gave %+v, want %+v", got, want)
	}
}

// Times come back in UTC whatever went in, because a comparison against a
// stored value that differed only by zone would fail in a way nobody could
// read.
func TestComputationTimesNormalizeToUTC(t *testing.T) {
	t.Parallel()

	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skipf("no zone database: %v", err)
	}

	want := store.Computation{
		ID:         hid.NewComputation(),
		CycleID:    id.MustNew("wxcyc"),
		ComputedAt: time.Date(2026, 8, 3, 7, 0, 0, 0, chicago),
	}

	got, err := computationFromModel(*computationToModel(want))
	if err != nil {
		t.Fatalf("computationFromModel: %v", err)
	}

	if got.ComputedAt.Location() != time.UTC {
		t.Errorf("computed_at came back in %v", got.ComputedAt.Location())
	}

	if !got.ComputedAt.Equal(want.ComputedAt) {
		t.Errorf("round trip moved the instant: %v then %v",
			want.ComputedAt, got.ComputedAt)
	}
}

// A stored identifier of the wrong type is a corrupt row, and it has to fail
// loudly at the mapper rather than flow onward as a plausible value.
func TestAStoredIdentifierOfTheWrongTypeIsRefused(t *testing.T) {
	t.Parallel()

	broken := assignmentModel{
		ID:            id.MustNew("span").String(),
		CarrierID:     id.MustNew("brch").String(),
		ConductorCode: "drake",
		Basis:         string(conductor.Surveyed),
	}

	if _, err := assignmentFromModel(broken); err == nil {
		t.Fatal("a span identifier read back as an assignment")
	}
}

// The series table's column list is not ours to choose: fabriq's Timescale
// adapter names exactly these six in its INSERT, and scope_id is the one most
// easily left out. Asserted against the DDL text so that dropping a column
// fails here rather than on the first write against a live database.
func TestTheSeriesTableCarriesEveryColumnBulkWriteNames(t *testing.T) {
	t.Parallel()

	ddl := seriesTable()[0]

	for _, column := range []string{
		"time", "tenant_id", "tag_id", "value", "quality", "scope_id",
	} {
		if !strings.Contains(ddl, column) {
			t.Errorf("the series table has no %s column", column)
		}
	}
}

// The stored column names the carrier, not a branch.
//
// The grove tag is the contract with the table, and the table is what a
// migration renamed. A tag still reading branch_id compiles, round-trips in
// memory, and fails only against a real database — which is the slowest
// possible place to find out.
func TestAssignmentModelNamesTheCarrierColumn(t *testing.T) {
	t.Parallel()

	field, ok := reflect.TypeOf(assignmentModel{}).FieldByName("CarrierID")
	if !ok {
		t.Fatal("assignmentModel has no CarrierID field")
	}

	if got := field.Tag.Get("grove"); !strings.HasPrefix(got, "carrier_id,") {
		t.Errorf("grove tag = %q, want it to name carrier_id", got)
	}
}
