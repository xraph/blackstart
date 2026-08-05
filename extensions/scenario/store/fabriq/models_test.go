package fabriq

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	cid "github.com/xraph/blackstart/extensions/cascade/id"
	crid "github.com/xraph/blackstart/extensions/contingency/id"
	sid "github.com/xraph/blackstart/extensions/scenario/id"
	"github.com/xraph/blackstart/extensions/scenario/scenario"
	"github.com/xraph/blackstart/extensions/scenario/store"
)

// The composite has to satisfy the interface. A compile-time assertion rather
// than a runtime one, so a method dropped in a refactor fails the build.
var _ store.Store = (*Store)(nil)

func aManifest() scenario.Manifest {
	return scenario.Manifest{
		ModelHash:    "sha256:abc123",
		WeatherCycle: "gfs-2026080400",
		ValidAt:      time.Date(2026, 8, 4, 18, 0, 0, 0, time.UTC),
		HazardConfig: "default",
		Policy:       scenario.PolicyExplicit,
		Branches:     []string{"branch:14", "branch:22"},
		Seed:         42,
	}
}

func aScenario() scenario.Scenario {
	at := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	return scenario.Scenario{
		ID:        sid.NewScenario(),
		Name:      "storm response",
		Manifest:  aManifest(),
		CreatedAt: at,
		UpdatedAt: at,
	}
}

// A scenario survives the round trip through its persistence shape.
//
// The mappers are hand-written in both directions, which is what makes this
// worth testing: a field added to the domain type and forgotten in one mapper
// compiles, stores, and reads back as a zero.
func TestScenarioRoundTrips(t *testing.T) {
	t.Parallel()

	want := aScenario()

	row, err := scenarioFromDomain(want)
	if err != nil {
		t.Fatalf("scenarioFromDomain: %v", err)
	}

	got, err := row.toDomain()
	if err != nil {
		t.Fatalf("toDomain: %v", err)
	}

	if got.ID != want.ID || got.Name != want.Name {
		t.Errorf("scalars round-tripped as %+v", got)
	}

	if !reflect.DeepEqual(got.Manifest, want.Manifest) {
		t.Errorf("manifest round-tripped as %+v, wrote %+v", got.Manifest, want.Manifest)
	}

	if !got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Errorf("timestamps round-tripped as %v/%v, wrote %v/%v",
			got.CreatedAt, got.UpdatedAt, want.CreatedAt, want.UpdatedAt)
	}
}

// A stored identifier of the wrong type is a corrupt row, and it has to fail
// loudly at the mapper rather than flow onward as a plausible scenario.
func TestAStoredScenarioIdentifierOfTheWrongTypeIsRefused(t *testing.T) {
	t.Parallel()

	row, err := scenarioFromDomain(aScenario())
	if err != nil {
		t.Fatalf("scenarioFromDomain: %v", err)
	}

	row.ID = sid.NewRun().String()

	if _, err := row.toDomain(); err == nil {
		t.Fatal("a run identifier read back as a scenario")
	}
}

func aRun() scenario.ScenarioRun {
	at := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	return scenario.ScenarioRun{
		ID:            sid.NewRun(),
		ScenarioID:    sid.NewScenario(),
		Manifest:      aManifest(),
		Status:        scenario.StatusQueued,
		SolverVersion: "",
		QueuedAt:      at,
	}
}

// A run survives the round trip through its persistence shape, including the
// frozen manifest — the value the whole extension exists to make
// reproducible.
func TestRunRoundTrips(t *testing.T) {
	t.Parallel()

	weatherRated, branchCount := 137, 2000

	want := aRun()
	want.Status = scenario.StatusComplete
	want.SweepRunID = crid.NewRun()
	want.CascadeRunID = cid.NewRun()
	want.WeatherRated = &weatherRated
	want.BranchCount = &branchCount
	want.SolverVersion = "0.1.0-test"
	want.StartedAt = want.QueuedAt.Add(time.Second)
	want.FinishedAt = want.QueuedAt.Add(30 * time.Second)

	row, err := runFromDomain(want)
	if err != nil {
		t.Fatalf("runFromDomain: %v", err)
	}

	got, err := row.toDomain()
	if err != nil {
		t.Fatalf("toDomain: %v", err)
	}

	if got.ID != want.ID || got.ScenarioID != want.ScenarioID || got.Status != want.Status {
		t.Errorf("scalars round-tripped as %+v", got)
	}

	if !reflect.DeepEqual(got.Manifest, want.Manifest) {
		t.Errorf("manifest round-tripped as %+v, wrote %+v", got.Manifest, want.Manifest)
	}

	if got.SweepRunID != want.SweepRunID || got.CascadeRunID != want.CascadeRunID {
		t.Errorf("child run ids round-tripped as %+v", got)
	}

	if got.WeatherRated == nil || got.BranchCount == nil {
		t.Fatalf("weather rated / branch count came back nil: %+v", got)
	}

	if *got.WeatherRated != *want.WeatherRated || *got.BranchCount != *want.BranchCount {
		t.Errorf("weather rated %d branch count %d, want %d %d",
			*got.WeatherRated, *got.BranchCount, *want.WeatherRated, *want.BranchCount)
	}

	if got.SolverVersion != want.SolverVersion {
		t.Errorf("solver version = %q, want %q", got.SolverVersion, want.SolverVersion)
	}

	if !got.QueuedAt.Equal(want.QueuedAt) || !got.StartedAt.Equal(want.StartedAt) ||
		!got.FinishedAt.Equal(want.FinishedAt) {
		t.Errorf("timestamps round-tripped as %+v", got)
	}
}

// A queued run has no failure, no child run ids, no rating counts and no
// started/finished timestamps — the ordinary, untouched state of a run
// nobody has claimed yet. None of those absences may survive the round trip
// as a zero value: a zero id.ID reads as a real (if empty) identifier to
// IsZero, and a zero time.Time reads as a real instant to anyone who calls
// IsZero on it, so this is the test that would catch either turning into the
// other.
func TestAbsentFieldsStayAbsent(t *testing.T) {
	t.Parallel()

	row, err := runFromDomain(aRun())
	if err != nil {
		t.Fatalf("runFromDomain: %v", err)
	}

	got, err := row.toDomain()
	if err != nil {
		t.Fatalf("toDomain: %v", err)
	}

	if got.Failure != nil {
		t.Errorf("a run with no failure came back with one: %+v", got.Failure)
	}

	if got.WeatherRated != nil || got.BranchCount != nil {
		t.Errorf("a run nobody rated came back rated: %+v", got)
	}

	if !got.SweepRunID.IsZero() || !got.CascadeRunID.IsZero() {
		t.Errorf("a queued run came back with child run ids: %+v", got)
	}

	if !got.StartedAt.IsZero() {
		t.Errorf("a run nobody claimed came back started: %v", got.StartedAt)
	}

	if !got.FinishedAt.IsZero() {
		t.Errorf("a run still queued came back finished: %v", got.FinishedAt)
	}
}

// WeatherRated pointing at zero is a real finding — a rating step that ran
// and rated none of the network's branches — and it must not collapse into
// "the rating step never ran" on the way through the database. That
// collapse is exactly what a bare int, or a pointer mishandled on either
// side of the mapper, would produce.
func TestWeatherRatedZeroRoundTrips(t *testing.T) {
	t.Parallel()

	zero := 0
	total := 2000

	want := aRun()
	want.WeatherRated = &zero
	want.BranchCount = &total

	row, err := runFromDomain(want)
	if err != nil {
		t.Fatalf("runFromDomain: %v", err)
	}

	got, err := row.toDomain()
	if err != nil {
		t.Fatalf("toDomain: %v", err)
	}

	if got.WeatherRated == nil {
		t.Fatal("weather rated came back nil, want a pointer to 0")
	}

	if *got.WeatherRated != 0 {
		t.Errorf("weather rated = %d, want 0", *got.WeatherRated)
	}

	if got.BranchCount == nil || *got.BranchCount != total {
		t.Errorf("branch count = %v, want %d", got.BranchCount, total)
	}
}

// A failed run's Failure must round-trip as a real value, not as a
// zero-valued struct standing in for a nil pointer.
func TestFailureRoundTrips(t *testing.T) {
	t.Parallel()

	want := aRun()
	want.Status = scenario.StatusFailed
	want.Failure = &scenario.Failure{
		Step:   scenario.StepScreen,
		Reason: "solver timed out",
	}

	row, err := runFromDomain(want)
	if err != nil {
		t.Fatalf("runFromDomain: %v", err)
	}

	got, err := row.toDomain()
	if err != nil {
		t.Fatalf("toDomain: %v", err)
	}

	if got.Failure == nil {
		t.Fatal("failure came back nil")
	}

	if got.Failure.Step != want.Failure.Step || got.Failure.Reason != want.Failure.Reason {
		t.Errorf("failure round-tripped as %+v, wrote %+v", got.Failure, want.Failure)
	}
}

// A stored identifier of the wrong type is a corrupt row, and it has to fail
// loudly at the mapper rather than flow onward as a plausible run.
func TestAStoredRunIdentifierOfTheWrongTypeIsRefused(t *testing.T) {
	t.Parallel()

	row, err := runFromDomain(aRun())
	if err != nil {
		t.Fatalf("runFromDomain: %v", err)
	}

	row.ID = sid.NewScenario().String()

	if _, err := row.toDomain(); err == nil {
		t.Fatal("a scenario identifier read back as a run")
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
	want.QueuedAt = time.Date(2026, 8, 4, 7, 0, 0, 0, chicago)
	want.StartedAt = want.QueuedAt.Add(time.Minute)

	row, err := runFromDomain(want)
	if err != nil {
		t.Fatalf("runFromDomain: %v", err)
	}

	got, err := row.toDomain()
	if err != nil {
		t.Fatalf("toDomain: %v", err)
	}

	if got.QueuedAt.Location() != time.UTC || got.StartedAt.Location() != time.UTC {
		t.Errorf("timestamps came back in %v / %v", got.QueuedAt.Location(), got.StartedAt.Location())
	}

	if !got.QueuedAt.Equal(want.QueuedAt) || !got.StartedAt.Equal(want.StartedAt) {
		t.Errorf("round trip moved the instant: %v/%v then %v/%v",
			want.QueuedAt, want.StartedAt, got.QueuedAt, got.StartedAt)
	}
}

// Every column a model tags must appear in its own table's migration DDL.
//
// Derived by reflection rather than from a written-out list, and that is the
// point: a field added to scenarioRow or runRow with no column to land in
// fails here without anyone remembering to update a list.
//
// Each model is checked against only its own CREATE TABLE statement, not
// both tables joined into one string. With two tables, joining them would
// let a column pass by coincidence: a stray `grove:"name"` added to runRow
// would match the scenario table's real name column and pass here, then fail
// at INSERT against the run table, which has no such column. Matching is by
// word boundary rather than substring for the same reason a tag of "tenant"
// must not appear to match "tenant_id" — they name different columns, and a
// substring check cannot tell them apart.
func TestEveryTaggedColumnExistsInTheSchema(t *testing.T) {
	t.Parallel()

	cases := []struct {
		model any
		ddl   string
	}{
		{scenarioRow{}, tableDDL(t, ScenarioTable)},
		{runRow{}, tableDDL(t, RunTable)},
	}

	for _, c := range cases {
		typ := reflect.TypeOf(c.model)

		for field := range typ.Fields() {
			tag := field.Tag.Get("grove")
			if tag == "" {
				continue
			}

			column, _, _ := strings.Cut(tag, ",")
			if column == "" || strings.HasPrefix(column, "table:") {
				continue
			}

			pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(column) + `\b`)
			if !pattern.MatchString(c.ddl) {
				t.Errorf("%s tags column %q, which its own table's migration does not create",
					typ.Name(), column)
			}
		}
	}
}

// tableDDL returns the single CREATE TABLE statement for table out of
// tables(), rather than every statement joined together — see
// TestEveryTaggedColumnExistsInTheSchema's own comment for why the join is
// exactly what must be avoided here.
func tableDDL(t *testing.T, table string) string {
	t.Helper()

	prefix := "CREATE TABLE IF NOT EXISTS " + table
	for _, stmt := range tables() {
		if strings.HasPrefix(stmt, prefix) {
			return stmt
		}
	}

	t.Fatalf("no CREATE TABLE statement found for %s", table)

	return ""
}
