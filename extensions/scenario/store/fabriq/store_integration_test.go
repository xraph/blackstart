//go:build integration

package fabriq_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/xraph/fabriq"

	cid "github.com/xraph/blackstart/extensions/cascade/id"
	crid "github.com/xraph/blackstart/extensions/contingency/id"
	sid "github.com/xraph/blackstart/extensions/scenario/id"
	"github.com/xraph/blackstart/extensions/scenario/scenario"
	scnfabriq "github.com/xraph/blackstart/extensions/scenario/store/fabriq"
	"github.com/xraph/blackstart/internal/entities"
	"github.com/xraph/blackstart/internal/testsupport"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

const tenantID = "blackstart"

var createdAt = time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

// harness brings up Postgres, runs the migration group, provisions the
// restricted application role, and opens Fabriq.
func harness(t *testing.T) *scnfabriq.Store {
	t.Helper()

	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, scnfabriq.Group())

	// The application connects as a non-superuser, because PostgreSQL row
	// security never constrains a superuser — a test that connected as one
	// would pass every isolation check while proving nothing.
	appDSN := testsupport.AppDSN(t, superDSN)

	reg, err := entities.Build(scnfabriq.Contributor{})
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

	s, err := scnfabriq.New(fab, tenantID)
	if err != nil {
		t.Fatalf("building the store: %v", err)
	}

	return s
}

func aManifest(modelHash string) scenario.Manifest {
	return scenario.Manifest{
		ModelHash:    modelHash,
		WeatherCycle: "gfs-2026080400",
		ValidAt:      createdAt,
		Policy:       scenario.PolicyAll,
		Seed:         7,
	}
}

func aScenario(modelHash string) scenario.Scenario {
	return scenario.Scenario{
		ID:        sid.NewScenario(),
		Name:      "storm response",
		Manifest:  aManifest(modelHash),
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}
}

func aQueuedRun(scenarioID id.ID, queuedAt time.Time) scenario.Run {
	return scenario.Run{
		ID:         sid.NewRun(),
		ScenarioID: scenarioID,
		Manifest:   aManifest("sha256:abc123"),
		Status:     scenario.StatusQueued,
		QueuedAt:   queuedAt,
	}
}

func TestScenarioRoundTripsThroughPostgres(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	want := aScenario("sha256:abc123")

	if _, err := s.Scenarios().Create(ctx, want); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Scenarios().Get(ctx, want.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.ID != want.ID || got.Name != want.Name {
		t.Errorf("read back %+v", got)
	}

	if got.Manifest.ModelHash != want.Manifest.ModelHash ||
		got.Manifest.WeatherCycle != want.Manifest.WeatherCycle {
		t.Errorf("manifest read back as %+v", got.Manifest)
	}
}

func TestASecondScenarioWriteToOneIdentifierConflicts(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	want := aScenario("sha256:once")

	if _, err := s.Scenarios().Create(ctx, want); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err := s.Scenarios().Create(ctx, want)
	if !errors.Is(err, bserr.ErrConflict) {
		t.Fatalf("second Create: %v, want bserr.ErrConflict", err)
	}
}

func TestAMissingScenarioIsNotFound(t *testing.T) {
	s := harness(t)

	_, err := s.Scenarios().Get(context.Background(), sid.NewScenario())
	if !errors.Is(err, bserr.ErrNotFound) {
		t.Fatalf("Get error = %v, want bserr.ErrNotFound", err)
	}
}

func TestRunRoundTripsThroughPostgres(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	sc := aScenario("sha256:run-roundtrip")
	if _, err := s.Scenarios().Create(ctx, sc); err != nil {
		t.Fatalf("creating the scenario: %v", err)
	}

	want := aQueuedRun(sc.ID, createdAt)

	if _, err := s.Runs().Create(ctx, want); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Runs().Get(ctx, want.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.ID != want.ID || got.ScenarioID != want.ScenarioID || got.Status != want.Status {
		t.Errorf("read back %+v", got)
	}

	if got.Failure != nil {
		t.Errorf("a queued run read back with a failure: %+v", got.Failure)
	}

	if got.WeatherRated != nil || got.BranchCount != nil {
		t.Errorf("a queued run read back rated: %+v", got)
	}

	if !got.StartedAt.IsZero() || !got.FinishedAt.IsZero() {
		t.Errorf("a queued run read back with timestamps: %+v", got)
	}
}

// TestRunRoundTripsThroughPostgres only ever writes a run with every
// absent-able field left absent. This is the other direction: every one of
// those fields populated, including WeatherRated pointing at 0 rather than
// some other number — the pointer-to-zero case models_test.go's
// TestWeatherRatedZeroRoundTrips already covers in-process, but never against
// the real INTEGER column. If grove wrote NULL for a pointer-to-zero, or
// scanned a stored 0 back as nil, every other test in this file would still
// pass and "rated zero of two thousand branches" would silently read back as
// "has not looked yet" — this is the one test that would catch it.
func TestRunWithEveryFieldSetRoundTripsThroughPostgres(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	sc := aScenario("sha256:run-fully-populated")
	if _, err := s.Scenarios().Create(ctx, sc); err != nil {
		t.Fatalf("creating the scenario: %v", err)
	}

	weatherRated, branchCount := 0, 2000

	want := aQueuedRun(sc.ID, createdAt)
	want.Status = scenario.StatusFailed
	want.Failure = &scenario.Failure{
		Step:   scenario.StepCascade,
		Reason: "solver timed out",
	}
	want.WeatherRated = &weatherRated
	want.BranchCount = &branchCount
	want.SweepRunID = crid.NewRun()
	want.CascadeRunID = cid.NewRun()
	want.SolverVersion = "0.1.0-test"
	want.StartedAt = createdAt.Add(time.Minute)
	want.FinishedAt = createdAt.Add(2 * time.Minute)

	if _, err := s.Runs().Create(ctx, want); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Runs().Get(ctx, want.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Failure == nil {
		t.Fatal("failure came back nil")
	}

	if got.Failure.Step != want.Failure.Step || got.Failure.Reason != want.Failure.Reason {
		t.Errorf("failure round-tripped as %+v, want %+v", got.Failure, want.Failure)
	}

	if got.WeatherRated == nil || got.BranchCount == nil {
		t.Fatalf("weather rated / branch count came back nil: %+v", got)
	}

	// Dereference and compare the value, not merely check non-nil: a
	// non-nil check alone would pass against an implementation that stored
	// some other number in place of the real 0.
	if *got.WeatherRated != weatherRated {
		t.Errorf("weather rated = %d, want %d", *got.WeatherRated, weatherRated)
	}

	if *got.BranchCount != branchCount {
		t.Errorf("branch count = %d, want %d", *got.BranchCount, branchCount)
	}

	if got.SweepRunID != want.SweepRunID || got.CascadeRunID != want.CascadeRunID {
		t.Errorf("child run ids round-tripped as %+v, want sweep=%s cascade=%s",
			got, want.SweepRunID, want.CascadeRunID)
	}

	if got.SolverVersion != want.SolverVersion {
		t.Errorf("solver version = %q, want %q", got.SolverVersion, want.SolverVersion)
	}

	if !got.StartedAt.Equal(want.StartedAt) {
		t.Errorf("started at = %v, want %v", got.StartedAt, want.StartedAt)
	}

	if !got.FinishedAt.Equal(want.FinishedAt) {
		t.Errorf("finished at = %v, want %v", got.FinishedAt, want.FinishedAt)
	}
}

func TestASecondRunWriteToOneIdentifierConflicts(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	sc := aScenario("sha256:run-conflict")
	if _, err := s.Scenarios().Create(ctx, sc); err != nil {
		t.Fatalf("creating the scenario: %v", err)
	}

	want := aQueuedRun(sc.ID, createdAt)

	if _, err := s.Runs().Create(ctx, want); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err := s.Runs().Create(ctx, want)
	if !errors.Is(err, bserr.ErrConflict) {
		t.Fatalf("second Create: %v, want bserr.ErrConflict", err)
	}
}

func TestAMissingRunIsNotFound(t *testing.T) {
	s := harness(t)

	_, err := s.Runs().Get(context.Background(), sid.NewRun())
	if !errors.Is(err, bserr.ErrNotFound) {
		t.Fatalf("Get error = %v, want bserr.ErrNotFound", err)
	}
}

// Most recent first, and only this scenario's runs.
func TestListByScenarioOrdersNewestFirst(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	sc := aScenario("sha256:listed")
	if _, err := s.Scenarios().Create(ctx, sc); err != nil {
		t.Fatalf("creating the scenario: %v", err)
	}

	other := aScenario("sha256:elsewhere")
	if _, err := s.Scenarios().Create(ctx, other); err != nil {
		t.Fatalf("creating the other scenario: %v", err)
	}

	for hour := range 3 {
		if _, err := s.Runs().Create(ctx,
			aQueuedRun(sc.ID, createdAt.Add(time.Duration(hour)*time.Hour)),
		); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	if _, err := s.Runs().Create(ctx, aQueuedRun(other.ID, createdAt)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Runs().ListByScenario(ctx, sc.ID, 0)
	if err != nil {
		t.Fatalf("ListByScenario: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("listed %d runs for one scenario, want 3", len(got))
	}

	for index := 1; index < len(got); index++ {
		if got[index].QueuedAt.After(got[index-1].QueuedAt) {
			t.Errorf("row %d is newer than the one before it", index)
		}
	}
}

func TestListByScenarioHonoursTheLimit(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	sc := aScenario("sha256:capped")
	if _, err := s.Scenarios().Create(ctx, sc); err != nil {
		t.Fatalf("creating the scenario: %v", err)
	}

	for hour := range 4 {
		if _, err := s.Runs().Create(ctx,
			aQueuedRun(sc.ID, createdAt.Add(time.Duration(hour)*time.Hour)),
		); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	got, err := s.Runs().ListByScenario(ctx, sc.ID, 2)
	if err != nil {
		t.Fatalf("ListByScenario: %v", err)
	}

	if len(got) != 2 {
		t.Errorf("a limit of 2 returned %d rows", len(got))
	}
}

// A limit of zero or less returns everything — scenario.RunStore documents
// that, and store.go's ListByScenario relies on it after commit 4dbf824
// removed a silent clamp to 50. A row count at or under 50 would pass this
// assertion whether or not the clamp were still there, which is why this
// creates more than 50 runs: it is the only way a reintroduced clamp would
// make this test fail.
func TestListByScenarioWithNoLimitReturnsEverything(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	sc := aScenario("sha256:unclamped")
	if _, err := s.Scenarios().Create(ctx, sc); err != nil {
		t.Fatalf("creating the scenario: %v", err)
	}

	const runCount = 60

	for hour := range runCount {
		if _, err := s.Runs().Create(ctx,
			aQueuedRun(sc.ID, createdAt.Add(time.Duration(hour)*time.Hour)),
		); err != nil {
			t.Fatalf("Create run %d: %v", hour, err)
		}
	}

	got, err := s.Runs().ListByScenario(ctx, sc.ID, 0)
	if err != nil {
		t.Fatalf("ListByScenario: %v", err)
	}

	if len(got) != runCount {
		t.Fatalf("a limit of 0 returned %d rows, want all %d", len(got), runCount)
	}
}

// Claim takes the oldest queued run and marks it running.
func TestClaimTakesTheOldestQueuedRun(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	sc := aScenario("sha256:claim-order")
	if _, err := s.Scenarios().Create(ctx, sc); err != nil {
		t.Fatalf("creating the scenario: %v", err)
	}

	older := aQueuedRun(sc.ID, createdAt)
	newer := aQueuedRun(sc.ID, createdAt.Add(time.Hour))

	if _, err := s.Runs().Create(ctx, newer); err != nil {
		t.Fatalf("creating the newer run: %v", err)
	}

	if _, err := s.Runs().Create(ctx, older); err != nil {
		t.Fatalf("creating the older run: %v", err)
	}

	claimed, err := s.Runs().Claim(ctx)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}

	if claimed.ID != older.ID {
		t.Fatalf("claimed run %s, want the older run %s", claimed.ID, older.ID)
	}

	if claimed.Status != scenario.StatusRunning {
		t.Errorf("claimed run has status %q, want running", claimed.Status)
	}

	if claimed.StartedAt.IsZero() {
		t.Error("claimed run has no started_at")
	}

	got, err := s.Runs().Get(ctx, older.ID)
	if err != nil {
		t.Fatalf("Get after claim: %v", err)
	}

	if got.Status != scenario.StatusRunning || got.StartedAt.IsZero() {
		t.Errorf("the claim did not persist: %+v", got)
	}
}

// Claiming with nothing queued is ErrNotFound, never a zero Run — a zero
// Run has an empty Status, which would read as a real run to a caller that
// only checked the error loosely.
func TestClaimWithNothingQueuedIsNotFound(t *testing.T) {
	s := harness(t)

	_, err := s.Runs().Claim(context.Background())
	if !errors.Is(err, bserr.ErrNotFound) {
		t.Fatalf("Claim error = %v, want bserr.ErrNotFound", err)
	}
}

// Claim does not return a run twice: once claimed, it is running and no
// longer a candidate.
func TestClaimDoesNotReturnTheSameRunTwice(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	sc := aScenario("sha256:claim-once")
	if _, err := s.Scenarios().Create(ctx, sc); err != nil {
		t.Fatalf("creating the scenario: %v", err)
	}

	run := aQueuedRun(sc.ID, createdAt)
	if _, err := s.Runs().Create(ctx, run); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := s.Runs().Claim(ctx); err != nil {
		t.Fatalf("first Claim: %v", err)
	}

	_, err := s.Runs().Claim(ctx)
	if !errors.Is(err, bserr.ErrNotFound) {
		t.Fatalf("second Claim error = %v, want bserr.ErrNotFound", err)
	}
}

// The property that matters: many workers claiming concurrently against a
// bounded queue never receive the same run twice, and every queued run is
// claimed exactly once. A SELECT followed by a separate UPDATE from Go would
// let two workers both read a run as queued before either wrote it running —
// this is the test that would catch that race, because a sequential test
// cannot: it would pass even if Claim read, released, and only then wrote.
func TestClaimIsAtomicUnderConcurrency(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	sc := aScenario("sha256:claim-concurrent")
	if _, err := s.Scenarios().Create(ctx, sc); err != nil {
		t.Fatalf("creating the scenario: %v", err)
	}

	const runCount = 20

	want := make(map[string]bool, runCount)

	for i := range runCount {
		run := aQueuedRun(sc.ID, createdAt.Add(time.Duration(i)*time.Second))
		if _, err := s.Runs().Create(ctx, run); err != nil {
			t.Fatalf("creating run %d: %v", i, err)
		}

		want[run.ID.String()] = true
	}

	const workerCount = 8

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		claimed = make(map[string]int)
	)

	for range workerCount {
		wg.Go(func() {
			for {
				run, err := s.Runs().Claim(ctx)
				if err != nil {
					// The queue is exhausted; every other worker eventually
					// sees the same and stops too.
					return
				}

				mu.Lock()
				claimed[run.ID.String()]++
				mu.Unlock()
			}
		})
	}

	wg.Wait()

	if len(claimed) != runCount {
		t.Fatalf("claimed %d distinct runs, want %d", len(claimed), runCount)
	}

	for runID, count := range claimed {
		if !want[runID] {
			t.Errorf("claimed a run nobody queued: %s", runID)
		}

		if count != 1 {
			t.Errorf("run %s was claimed %d times, want exactly 1", runID, count)
		}
	}
}

// Updating a stored scenario replaces it in Postgres, and the replacement is
// what a later Get returns.
func TestUpdateReplacesAStoredScenario(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	sc := aScenario("sha256:before-the-edit")
	if _, err := s.Scenarios().Create(ctx, sc); err != nil {
		t.Fatalf("Create: %v", err)
	}

	edited := sc
	edited.Name = "renamed"
	edited.Manifest.ModelHash = "sha256:after-the-edit"
	edited.UpdatedAt = createdAt.Add(time.Hour)

	if _, err := s.Scenarios().Update(ctx, edited); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := s.Scenarios().Get(ctx, sc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Name != edited.Name || got.Manifest.ModelHash != edited.Manifest.ModelHash {
		t.Errorf("the edit did not land: %q / %q", got.Name, got.Manifest.ModelHash)
	}
}

// An Update naming a scenario nobody defined is not found rather than a
// quiet insert. This pins fabriq's contract rather than this store's code —
// the refusal comes from the executor having no version for the aggregate —
// and it is the half of the rule the memory store has to implement itself.
func TestUpdateRefusesAScenarioThatDoesNotExist(t *testing.T) {
	s := harness(t)

	_, err := s.Scenarios().Update(context.Background(), aScenario("sha256:never-defined"))
	if !errors.Is(err, bserr.ErrNotFound) {
		t.Fatalf("Update error = %v, want bserr.ErrNotFound", err)
	}
}

// Listing returns scenarios newest first, and a limit of zero returns every
// one of them rather than some store-chosen default.
func TestListOrdersScenariosNewestFirst(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	for hour := range 3 {
		sc := aScenario("sha256:listed")
		sc.CreatedAt = createdAt.Add(time.Duration(hour) * time.Hour)
		sc.UpdatedAt = sc.CreatedAt

		if _, err := s.Scenarios().Create(ctx, sc); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	got, err := s.Scenarios().List(ctx, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("listed %d scenarios, want 3 — a limit of zero means everything", len(got))
	}

	for index := 1; index < len(got); index++ {
		if got[index].CreatedAt.After(got[index-1].CreatedAt) {
			t.Errorf("row %d is newer than the one before it", index)
		}
	}

	limited, err := s.Scenarios().List(ctx, 2)
	if err != nil {
		t.Fatalf("List(2): %v", err)
	}

	if len(limited) != 2 {
		t.Errorf("listed %d scenarios for a limit of 2", len(limited))
	}
}

// aClaimedRun stores a queued run and claims it, which is the only state
// Finish accepts.
func aClaimedRun(t *testing.T, s *scnfabriq.Store, modelHash string) scenario.Run {
	t.Helper()

	ctx := context.Background()

	sc := aScenario(modelHash)
	if _, err := s.Scenarios().Create(ctx, sc); err != nil {
		t.Fatalf("creating the scenario: %v", err)
	}

	if _, err := s.Runs().Create(ctx, aQueuedRun(sc.ID, createdAt)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	claimed, err := s.Runs().Claim(ctx)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}

	return claimed
}

// A finished run's terminal state reaches Postgres — the failure with its
// step, the child identifiers, the coverage counts and the finish time — and
// comes back the same. Without this the run stays running forever and the
// answer it produced is nowhere.
func TestFinishRecordsATerminalStateInPostgres(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	claimed := aClaimedRun(t, s, "sha256:finished")

	rated, branches := 3, 5
	claimed.Status = scenario.StatusComplete
	claimed.SweepRunID = crid.NewRun()
	claimed.CascadeRunID = cid.NewRun()
	claimed.WeatherRated = &rated
	claimed.BranchCount = &branches
	claimed.SolverVersion = "bs-solver 0.4.1"
	claimed.FinishedAt = createdAt.Add(2 * time.Minute)

	if _, err := s.Runs().Finish(ctx, claimed); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	got, err := s.Runs().Get(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Status != scenario.StatusComplete {
		t.Errorf("status = %q, want complete; Finish did not persist", got.Status)
	}

	if got.SweepRunID != claimed.SweepRunID || got.CascadeRunID != claimed.CascadeRunID {
		t.Errorf("child ids read back as %s / %s", got.SweepRunID, got.CascadeRunID)
	}

	if got.WeatherRated == nil || *got.WeatherRated != rated {
		t.Errorf("weather rated read back as %v, want %d", got.WeatherRated, rated)
	}

	if got.SolverVersion != claimed.SolverVersion {
		t.Errorf("solver version = %q", got.SolverVersion)
	}

	if !got.FinishedAt.Equal(claimed.FinishedAt) {
		t.Errorf("finished at = %v, want %v", got.FinishedAt, claimed.FinishedAt)
	}
}

// A failed run's step and reason survive the JSONB column.
func TestFinishRecordsWhichStepFailed(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	claimed := aClaimedRun(t, s, "sha256:failed")

	zero := 0
	branches := 12
	claimed.Status = scenario.StatusFailed
	claimed.Failure = &scenario.Failure{
		Step:   scenario.StepRatings,
		Reason: "cycle wxcyc_1 has computed no ampacity for 2026-08-04T18:00:00Z",
	}
	// Rated zero of twelve, which is a finding rather than an absence: a run
	// that failed at the rating step after rating nothing must not read the
	// same as one that never reached the step.
	claimed.WeatherRated = &zero
	claimed.BranchCount = &branches
	claimed.FinishedAt = createdAt.Add(time.Minute)

	if _, err := s.Runs().Finish(ctx, claimed); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	got, err := s.Runs().Get(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Failure == nil {
		t.Fatal("the failure did not survive the round trip")
	}

	if got.Failure.Step != scenario.StepRatings || got.Failure.Reason != claimed.Failure.Reason {
		t.Errorf("failure read back as %+v", got.Failure)
	}

	if got.WeatherRated == nil || *got.WeatherRated != 0 {
		t.Errorf("weather rated read back as %v, want a pointer to 0 — nil would say "+
			"the rating step never ran", got.WeatherRated)
	}
}

// A run that is not running cannot finish. A second worker that believed it
// owned a run someone else already finished must be told, not allowed to
// write its own answer over the first one's.
func TestFinishRefusesARunThatIsNotRunning(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	claimed := aClaimedRun(t, s, "sha256:twice")
	claimed.Status = scenario.StatusFailed
	claimed.Failure = &scenario.Failure{Step: scenario.StepCascade, Reason: "the solver refused"}
	claimed.FinishedAt = createdAt.Add(time.Minute)

	if _, err := s.Runs().Finish(ctx, claimed); err != nil {
		t.Fatalf("first Finish: %v", err)
	}

	if _, err := s.Runs().Finish(ctx, claimed); !errors.Is(err, bserr.ErrConflict) {
		t.Fatalf("second Finish: %v, want bserr.ErrConflict", err)
	}
}

// Finishing a run nothing recorded is not found, not a quiet insert.
func TestFinishRefusesARunThatDoesNotExist(t *testing.T) {
	s := harness(t)

	orphan := aQueuedRun(sid.NewScenario(), createdAt)
	orphan.Status = scenario.StatusFailed
	orphan.Failure = &scenario.Failure{Step: scenario.StepResolve, Reason: "no such model"}

	if _, err := s.Runs().Finish(context.Background(), orphan); !errors.Is(err, bserr.ErrNotFound) {
		t.Fatalf("Finish error = %v, want bserr.ErrNotFound", err)
	}
}

// Finish cannot move the frozen manifest, and here that means it cannot
// rewrite the manifest column.
//
// Finish writes a whole row from an argument a caller has been holding.
// Today that argument is always Claim's output, but a retry endpoint or a
// stuck-run reaper would hand back something else, and a manifest arriving
// that way would replace the frozen one — leaving a row that is internally
// consistent and still looks citable. The memory store enforces the same
// rule; this is the half that has to survive a JSONB round trip.
func TestFinishCannotMoveTheFrozenManifest(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	claimed := aClaimedRun(t, s, "sha256:frozen")

	tampered := claimed
	tampered.Manifest.ModelHash = "sha256:a-different-network"
	tampered.Manifest.Seed = 999
	tampered.ScenarioID = sid.NewScenario()
	tampered.QueuedAt = claimed.QueuedAt.Add(72 * time.Hour)
	tampered.Status = scenario.StatusComplete
	tampered.SweepRunID = crid.NewRun()
	tampered.CascadeRunID = cid.NewRun()
	tampered.FinishedAt = createdAt.Add(time.Minute)

	returned, err := s.Runs().Finish(ctx, tampered)
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}

	got, err := s.Runs().Get(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Manifest.ModelHash != claimed.Manifest.ModelHash ||
		got.Manifest.Seed != claimed.Manifest.Seed {
		t.Errorf("the stored manifest moved to %+v, want the frozen %+v",
			got.Manifest, claimed.Manifest)
	}

	if got.ScenarioID != claimed.ScenarioID {
		t.Errorf("the run now belongs to scenario %s, want %s",
			got.ScenarioID, claimed.ScenarioID)
	}

	if !got.QueuedAt.Equal(claimed.QueuedAt) {
		t.Errorf("the queue time moved to %v", got.QueuedAt)
	}

	// What Finish returns is what the store now holds, not the argument it
	// was handed — a caller acting on the return value must not be looking at
	// a manifest the database rejected.
	if returned.Manifest.ModelHash != got.Manifest.ModelHash {
		t.Errorf("Finish returned manifest %q while storing %q",
			returned.Manifest.ModelHash, got.Manifest.ModelHash)
	}

	if got.Status != scenario.StatusComplete || got.SweepRunID != tampered.SweepRunID {
		t.Errorf("Finish did not record the outcome: %+v", got)
	}
}

// Finish is not a way back into the queue. Writing a queued status over a run
// that already produced an answer would hand it to a second worker to execute
// again. The memory store refuses it too; both delegate to Run.Finishable,
// and store parity is the thing this plan has already been burned on.
func TestFinishRefusesANonTerminalStatus(t *testing.T) {
	s := harness(t)

	claimed := aClaimedRun(t, s, "sha256:not-terminal")
	claimed.Status = scenario.StatusQueued

	if _, err := s.Runs().Finish(context.Background(), claimed); !errors.Is(err, bserr.ErrInvalid) {
		t.Fatalf("Finish error = %v, want bserr.ErrInvalid", err)
	}
}
