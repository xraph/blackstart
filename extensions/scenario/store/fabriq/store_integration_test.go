//go:build integration

package fabriq_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/xraph/fabriq"

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
