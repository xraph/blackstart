package fabriq

import (
	"reflect"
	"strings"
	"testing"
	"time"

	cid "github.com/xraph/blackstart/extensions/cascade/id"
	"github.com/xraph/blackstart/extensions/cascade/run"
	"github.com/xraph/blackstart/extensions/cascade/store"
)

// The composite has to satisfy the interface. A compile-time assertion rather
// than a runtime one, so a method dropped in a refactor fails the build.
var _ store.Store = (*Store)(nil)

func aRun() run.Run {
	at := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	frequency, nadir, rocof := 59.4, 58.1, -1.8
	first := uint32(0)

	return run.Run{
		ID:            cid.NewRun(),
		ModelHash:     "sha256:abc123",
		SolverVersion: "0.1.0-test",
		Outcome:       run.OutcomeTruncated,
		Events: []run.Event{
			{At: 0, Kind: run.EventOverload, Element: "branch:14", Detail: "118%"},
			{At: 2.5, Kind: run.EventTrip, Element: "branch:14"},
			{At: 2.5, Kind: run.EventIsland, Detail: "split into 2"},
			{At: 9, Kind: run.EventShed, Element: "bus:31", Island: &first},
		},
		Islands: []run.Island{
			{
				Buses: []uint32{0, 1, 2}, BusCount: 3,
				Condition: run.ConditionShedding, Verdict: run.VerdictViolated,
				Balance:   -0.12,
				Frequency: &frequency, Nadir: &nadir, Rocof: &rocof,
				Shed: 0.08,
			},
			{
				Buses: []uint32{3, 4}, BusCount: 2,
				Condition: run.ConditionStable, Verdict: run.VerdictSecure,
			},
		},
		Initiating: []string{"branch:14", "branch:22"},
		LoadLost:   0.08,
		Steps:      4,
		Elapsed:    12.5,
		StartedAt:  at,
		FinishedAt: at.Add(3 * time.Second),
	}
}

// A run survives the round trip through its persistence shape.
//
// The mappers are hand-written in both directions, which is what makes this
// worth testing: a field added to the domain type and forgotten in one mapper
// compiles, stores, and reads back as a zero.
func TestRunRoundTrips(t *testing.T) {
	t.Parallel()

	want := aRun()

	row, err := fromDomain(want)
	if err != nil {
		t.Fatalf("fromDomain: %v", err)
	}

	got, err := row.toDomain()
	if err != nil {
		t.Fatalf("toDomain: %v", err)
	}

	if got.ID != want.ID || got.ModelHash != want.ModelHash ||
		got.SolverVersion != want.SolverVersion || got.Outcome != want.Outcome {
		t.Errorf("scalars round-tripped as %+v", got)
	}

	if got.LoadLost != want.LoadLost || got.Steps != want.Steps ||
		got.Elapsed != want.Elapsed {
		t.Errorf("load %v steps %v elapsed %v, want %v %v %v",
			got.LoadLost, got.Steps, got.Elapsed,
			want.LoadLost, want.Steps, want.Elapsed)
	}

	if len(got.Events) != len(want.Events) {
		t.Fatalf("read back %d events, wrote %d", len(got.Events), len(want.Events))
	}

	for index, event := range got.Events {
		if event.Kind != want.Events[index].Kind ||
			event.At != want.Events[index].At ||
			event.Element != want.Events[index].Element {
			t.Errorf("event %d round-tripped as %+v", index, event)
		}
	}

	if len(got.Islands) != len(want.Islands) {
		t.Fatalf("read back %d islands, wrote %d", len(got.Islands), len(want.Islands))
	}

	// The field a concurrent change added after these mappers were written.
	// It is the whole reason this test exists.
	if len(got.Initiating) != len(want.Initiating) {
		t.Fatalf("read back %d initiating outages, wrote %d",
			len(got.Initiating), len(want.Initiating))
	}

	for index, outage := range got.Initiating {
		if outage != want.Initiating[index] {
			t.Errorf("initiating %d round-tripped as %q, wrote %q",
				index, outage, want.Initiating[index])
		}
	}
}

// The pointer fields are the ones a naive round trip flattens. An island with
// no machine data has no frequency to report, which is not the same as a
// frequency of zero — and an event before any split is not an event on island
// zero.
func TestAbsentPointersStayAbsent(t *testing.T) {
	t.Parallel()

	row, err := fromDomain(aRun())
	if err != nil {
		t.Fatalf("fromDomain: %v", err)
	}

	got, err := row.toDomain()
	if err != nil {
		t.Fatalf("toDomain: %v", err)
	}

	// The second island has no machine data.
	quiet := got.Islands[1]
	if quiet.Frequency != nil || quiet.Nadir != nil || quiet.Rocof != nil {
		t.Errorf("an island with no machines came back with a frequency: %+v", quiet)
	}

	// The first island does.
	loud := got.Islands[0]
	if loud.Frequency == nil || *loud.Frequency != 59.4 {
		t.Errorf("island 0 frequency = %v", loud.Frequency)
	}

	// The first three events predate the split.
	for index := range 3 {
		if got.Events[index].Island != nil {
			t.Errorf("event %d claims island %d before any split existed",
				index, *got.Events[index].Island)
		}
	}

	if got.Events[3].Island == nil || *got.Events[3].Island != 0 {
		t.Errorf("the shed event lost its island: %+v", got.Events[3])
	}
}

// A settled cascade sheds nothing and takes no steps worth counting. Those are
// zero values, and the model's fields carry no notnull tag for exactly that
// reason — fabriq refuses a zero for a notnull field, so tagging them would
// make the ordinary good case unstorable.
func TestASettledRunWithZeroesRoundTrips(t *testing.T) {
	t.Parallel()

	want := run.Run{
		ID:            cid.NewRun(),
		ModelHash:     "sha256:quiet",
		SolverVersion: "0.1.0-test",
		Outcome:       run.OutcomeSettled,
		StartedAt:     time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC),
		FinishedAt:    time.Date(2026, 8, 4, 12, 0, 1, 0, time.UTC),
	}

	row, err := fromDomain(want)
	if err != nil {
		t.Fatalf("fromDomain: %v", err)
	}

	got, err := row.toDomain()
	if err != nil {
		t.Fatalf("toDomain: %v", err)
	}

	if got.LoadLost != 0 || got.Steps != 0 || got.Elapsed != 0 {
		t.Errorf("a settled run round-tripped as %+v", got)
	}

	if len(got.Events) != 0 || len(got.Islands) != 0 {
		t.Errorf("empty collections came back as %d events and %d islands",
			len(got.Events), len(got.Islands))
	}
}

// Times come back in UTC whatever went in, because a comparison against a
// stored value that differed only by zone would fail in a way nobody could
// read.
func TestTimesNormalizeToUTC(t *testing.T) {
	t.Parallel()

	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skipf("no zone database: %v", err)
	}

	want := aRun()
	want.StartedAt = time.Date(2026, 8, 4, 7, 0, 0, 0, chicago)

	row, err := fromDomain(want)
	if err != nil {
		t.Fatalf("fromDomain: %v", err)
	}

	got, err := row.toDomain()
	if err != nil {
		t.Fatalf("toDomain: %v", err)
	}

	if got.StartedAt.Location() != time.UTC {
		t.Errorf("started_at came back in %v", got.StartedAt.Location())
	}

	if !got.StartedAt.Equal(want.StartedAt) {
		t.Errorf("round trip moved the instant: %v then %v",
			want.StartedAt, got.StartedAt)
	}
}

// A stored identifier of the wrong type is a corrupt row, and it has to fail
// loudly at the mapper rather than flow onward as a plausible run.
func TestAStoredIdentifierOfTheWrongTypeIsRefused(t *testing.T) {
	t.Parallel()

	row, err := fromDomain(aRun())
	if err != nil {
		t.Fatalf("fromDomain: %v", err)
	}

	row.ID = "nmdl_01k3qf8m2xw7rb4n09ytvdhjkm"

	if _, err := row.toDomain(); err == nil {
		t.Fatal("a network-model identifier read back as a cascade run")
	}
}

// Every column the model tags must appear in the migration DDL.
//
// Derived by reflection rather than from a written-out list, and that is the
// point: a field added to runRow with no column to land in fails here without
// anyone remembering to update a list. A concurrent change adding
// run.Initiating after these mappers were written is exactly how that happens.
func TestEveryTaggedColumnExistsInTheSchema(t *testing.T) {
	t.Parallel()

	ddl := strings.Join(tables(), "\n") + "\n" + strings.Join(initiatingColumn(), "\n")

	for field := range reflect.TypeFor[runRow]().Fields() {
		tag := field.Tag.Get("grove")
		if tag == "" {
			continue
		}

		column, _, _ := strings.Cut(tag, ",")
		if column == "" || strings.HasPrefix(column, "table:") {
			continue
		}

		if !strings.Contains(ddl, column) {
			t.Errorf("runRow tags column %q, which no migration creates", column)
		}
	}
}
