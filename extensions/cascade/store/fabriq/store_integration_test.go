//go:build integration

package fabriq_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xraph/fabriq"

	cid "github.com/xraph/blackstart/extensions/cascade/id"
	"github.com/xraph/blackstart/extensions/cascade/run"
	cscfabriq "github.com/xraph/blackstart/extensions/cascade/store/fabriq"
	"github.com/xraph/blackstart/internal/entities"
	"github.com/xraph/blackstart/internal/testsupport"
	"github.com/xraph/blackstart/pkg/bserr"
)

const tenantID = "blackstart"

var startedAt = time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

// harness brings up Postgres, runs the migration group, provisions the
// restricted application role, and opens Fabriq.
//
// Only this extension's group: a cascade run declares no edge to anything, so
// the registry has no other spec to resolve against.
func harness(t *testing.T) *cscfabriq.Store {
	t.Helper()

	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, cscfabriq.Group())

	// The application connects as a non-superuser, because PostgreSQL row
	// security never constrains a superuser — a test that connected as one
	// would pass every isolation check while proving nothing.
	appDSN := testsupport.AppDSN(t, superDSN)

	reg, err := entities.Build(cscfabriq.Contributor{})
	if err != nil {
		t.Fatalf("building the registry: %v", err)
	}

	fab, _, err := fabriq.Open(context.Background(), reg, fabriq.Config{
		Postgres: fabriq.PostgresConfig{DSN: appDSN},
	})
	if err != nil {
		t.Fatalf("opening fabriq: %v", err)
	}

	t.Cleanup(func() { _ = fab.Close() })

	s, err := cscfabriq.New(fab, tenantID)
	if err != nil {
		t.Fatalf("building the store: %v", err)
	}

	return s
}

func aRun(modelHash string, at time.Time) run.CascadeRun {
	frequency, nadir, rocof := 59.4, 58.1, -1.8
	first := uint32(0)

	return run.CascadeRun{
		ID:            cid.NewRun(),
		ModelHash:     modelHash,
		SolverVersion: "0.1.0-test",
		Outcome:       run.OutcomeTruncated,
		Events: []run.Event{
			{At: 0, Kind: run.EventOverload, Element: "branch:14", Detail: "118%"},
			{At: 2.5, Kind: run.EventTrip, Element: "branch:14"},
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
		Steps:      3,
		Elapsed:    12.5,
		StartedAt:  at,
		FinishedAt: at.Add(3 * time.Second),
	}
}

func TestRunRoundTripsThroughPostgres(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	want := aRun("sha256:abc123", startedAt)

	if _, err := s.Runs().Create(ctx, want); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Runs().Get(ctx, want.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.ID != want.ID || got.Outcome != want.Outcome ||
		got.LoadLost != want.LoadLost || got.Steps != want.Steps {
		t.Errorf("read back %+v", got)
	}

	if len(got.Events) != len(want.Events) {
		t.Errorf("read back %d events, wrote %d", len(got.Events), len(want.Events))
	}

	if len(got.Islands) != len(want.Islands) {
		t.Errorf("read back %d islands, wrote %d", len(got.Islands), len(want.Islands))
	}

	if len(got.Initiating) != len(want.Initiating) {
		t.Errorf("read back %d initiating outages, wrote %d",
			len(got.Initiating), len(want.Initiating))
	}
}

// The nested collections go through JSONB, so this is the test that would fail
// if the column type or the encoding changed underneath: a pointer that came
// back non-nil would mean an island with no machines had acquired a frequency.
func TestNestedShapesSurvivePostgres(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	want := aRun("sha256:nested", startedAt)

	if _, err := s.Runs().Create(ctx, want); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Runs().Get(ctx, want.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Islands[1].Frequency != nil {
		t.Errorf("an island with no machines came back at %v Hz",
			*got.Islands[1].Frequency)
	}

	if got.Islands[0].Frequency == nil || *got.Islands[0].Frequency != 59.4 {
		t.Errorf("island 0 frequency = %v", got.Islands[0].Frequency)
	}

	if got.Events[0].Island != nil {
		t.Error("an event before the split claims an island")
	}

	if got.Events[2].Island == nil || *got.Events[2].Island != 0 {
		t.Errorf("the shed event lost its island: %+v", got.Events[2])
	}
}

// A settled cascade sheds nothing and takes no steps. Those zeroes are why the
// model leaves notnull off three fields — fabriq refuses a zero for a notnull
// field, and the good case would otherwise be the unstorable one.
func TestASettledRunStores(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	want := run.CascadeRun{
		ID:            cid.NewRun(),
		ModelHash:     "sha256:quiet",
		SolverVersion: "0.1.0-test",
		Outcome:       run.OutcomeSettled,
		StartedAt:     startedAt,
		FinishedAt:    startedAt.Add(time.Second),
	}

	if _, err := s.Runs().Create(ctx, want); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Runs().Get(ctx, want.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.LoadLost != 0 || got.Steps != 0 || got.Elapsed != 0 {
		t.Errorf("a settled run read back as %+v", got)
	}
}

// Runs are append-only. An overwrite would replace a figure someone may
// already have acted on, so the second write is refused rather than merged.
func TestASecondWriteToOneIdentifierConflicts(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	want := aRun("sha256:once", startedAt)

	if _, err := s.Runs().Create(ctx, want); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err := s.Runs().Create(ctx, want)
	if !errors.Is(err, bserr.ErrConflict) {
		t.Fatalf("second Create: %v, want bserr.ErrConflict", err)
	}
}

// Most recent first, and only this network's runs.
func TestListByModelOrdersNewestFirst(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	for hour := range 3 {
		if _, err := s.Runs().Create(ctx,
			aRun("sha256:listed", startedAt.Add(time.Duration(hour)*time.Hour)),
		); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	if _, err := s.Runs().Create(ctx, aRun("sha256:elsewhere", startedAt)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Runs().ListByModel(ctx, "sha256:listed", 0)
	if err != nil {
		t.Fatalf("ListByModel: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("listed %d runs for one model, want 3", len(got))
	}

	for index := 1; index < len(got); index++ {
		if got[index].StartedAt.After(got[index-1].StartedAt) {
			t.Errorf("row %d is newer than the one before it", index)
		}
	}
}

func TestListByModelHonoursTheLimit(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	for hour := range 4 {
		if _, err := s.Runs().Create(ctx,
			aRun("sha256:capped", startedAt.Add(time.Duration(hour)*time.Hour)),
		); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	got, err := s.Runs().ListByModel(ctx, "sha256:capped", 2)
	if err != nil {
		t.Fatalf("ListByModel: %v", err)
	}

	if len(got) != 2 {
		t.Errorf("a limit of 2 returned %d rows", len(got))
	}
}

// The SQL predicate matches either hash, and a NULL canonical hash matches
// nothing.
//
// The Go side of this rule is covered by run.CascadeRun.NamesModel's own test;
// this is the half that only Postgres can answer, and the half where "absent"
// is a NULL rather than a nil pointer. Reverting the WHERE clause to
// `model_hash = $1` fails the canonical lookup here — the SQL is the only place
// a listing against a real database is decided.
func TestListByModelMatchesEitherHash(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	canonical := "sha256:canonical-side"

	both := aRun("sha256:protobuf-side", startedAt)
	both.CanonicalHash = &canonical

	if _, err := s.Runs().Create(ctx, both); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// A run from before the column: canonical_hash lands as NULL.
	older := aRun("sha256:protobuf-only", startedAt.Add(time.Hour))
	if _, err := s.Runs().Create(ctx, older); err != nil {
		t.Fatalf("Create (older): %v", err)
	}

	for _, hash := range []string{"sha256:protobuf-side", canonical} {
		got, err := s.Runs().ListByModel(ctx, hash, 0)
		if err != nil {
			t.Fatalf("ListByModel(%q): %v", hash, err)
		}

		if len(got) != 1 || got[0].ID != both.ID {
			t.Errorf("listing by %q returned %d runs, want the one that carries it",
				hash, len(got))
		}
	}

	// The older run is still findable by the only hash it has, and its absent
	// canonical hash comes back absent rather than as an empty string.
	got, err := s.Runs().ListByModel(ctx, "sha256:protobuf-only", 0)
	if err != nil {
		t.Fatalf("ListByModel (older): %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("listing a run that predates the column returned %d rows, want 1", len(got))
	}

	if got[0].CanonicalHash != nil {
		t.Errorf("a NULL canonical hash read back as %q; absent must stay absent",
			*got[0].CanonicalHash)
	}
}

// Every run names the network it simulated, so a listing with no model hash is
// a question with no answer rather than a request for everything.
func TestListByModelNeedsAModel(t *testing.T) {
	s := harness(t)

	if _, err := s.Runs().ListByModel(context.Background(), "", 10); err == nil {
		t.Fatal("listing with no model hash was accepted")
	}
}

func TestAMissingRunIsNotFound(t *testing.T) {
	s := harness(t)

	_, err := s.Runs().Get(context.Background(), cid.NewRun())
	if !errors.Is(err, bserr.ErrNotFound) {
		t.Fatalf("Get error = %v, want bserr.ErrNotFound", err)
	}
}
