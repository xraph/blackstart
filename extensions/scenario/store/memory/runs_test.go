package memory_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	scnid "github.com/xraph/blackstart/extensions/scenario/id"
	"github.com/xraph/blackstart/extensions/scenario/scenario"
	"github.com/xraph/blackstart/extensions/scenario/store/memory"
	"github.com/xraph/blackstart/pkg/bserr"
)

// A stored run round-trips with its manifest intact — the frozen copy is not
// something Create or Get is allowed to touch.
func TestAStoredRunRoundTripsWithItsManifestIntact(t *testing.T) {
	t.Parallel()

	s := memory.New()
	ctx := context.Background()

	in := scenario.Run{
		ID:         scnid.NewRun(),
		ScenarioID: scnid.NewScenario(),
		Manifest:   validManifest(),
		Status:     scenario.StatusQueued,
		QueuedAt:   time.Now().UTC(),
	}

	if _, err := s.Runs().Create(ctx, in); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Runs().Get(ctx, in.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.ScenarioID != in.ScenarioID {
		t.Errorf("scenario id did not round-trip: got %s, want %s", got.ScenarioID, in.ScenarioID)
	}

	if !reflect.DeepEqual(got.Manifest, in.Manifest) {
		t.Errorf("manifest did not round-trip: got %+v, want %+v", got.Manifest, in.Manifest)
	}
}

// Listing runs for a scenario returns them most recent first, and leaves out
// runs that belong to a different scenario entirely.
func TestListByScenarioReturnsMostRecentFirst(t *testing.T) {
	t.Parallel()

	s := memory.New()
	ctx := context.Background()

	scenarioID := scnid.NewScenario()
	base := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)

	var wantOrder []string

	for i := range 3 {
		run := scenario.Run{
			ID:         scnid.NewRun(),
			ScenarioID: scenarioID,
			Manifest:   validManifest(),
			Status:     scenario.StatusQueued,
			QueuedAt:   base.Add(time.Duration(i) * time.Hour),
		}

		if _, err := s.Runs().Create(ctx, run); err != nil {
			t.Fatalf("Create run %d: %v", i, err)
		}

		wantOrder = append(wantOrder, run.ID.String())
	}

	// A run queued after all three, but for a different scenario, must not
	// appear in this scenario's listing no matter how recent it is.
	other := scenario.Run{
		ID:         scnid.NewRun(),
		ScenarioID: scnid.NewScenario(),
		Manifest:   validManifest(),
		Status:     scenario.StatusQueued,
		QueuedAt:   base.Add(10 * time.Hour),
	}

	if _, err := s.Runs().Create(ctx, other); err != nil {
		t.Fatalf("Create unrelated run: %v", err)
	}

	got, err := s.Runs().ListByScenario(ctx, scenarioID, 0)
	if err != nil {
		t.Fatalf("ListByScenario: %v", err)
	}

	if len(got) != len(wantOrder) {
		t.Fatalf("got %d runs, want %d", len(got), len(wantOrder))
	}

	for i, run := range got {
		want := wantOrder[len(wantOrder)-1-i]
		if run.ID.String() != want {
			t.Errorf("position %d: got run %s, want %s (most recent first)", i, run.ID, want)
		}
	}
}

// Claiming a queued run returns it and moves it to running, and the move is
// visible to a later Get — Claim is not just handing back a mutated copy
// while leaving the stored record untouched.
func TestClaimTakesTheOldestQueuedRunAndMovesItToRunning(t *testing.T) {
	t.Parallel()

	s := memory.New()
	ctx := context.Background()

	in := scenario.Run{
		ID:         scnid.NewRun(),
		ScenarioID: scnid.NewScenario(),
		Manifest:   validManifest(),
		Status:     scenario.StatusQueued,
		QueuedAt:   time.Now().UTC(),
	}

	if _, err := s.Runs().Create(ctx, in); err != nil {
		t.Fatalf("Create: %v", err)
	}

	claimed, err := s.Runs().Claim(ctx)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}

	if claimed.ID != in.ID {
		t.Fatalf("Claim returned run %s, want %s", claimed.ID, in.ID)
	}

	if claimed.Status != scenario.StatusRunning {
		t.Errorf("claimed run has status %q, want %q", claimed.Status, scenario.StatusRunning)
	}

	if claimed.StartedAt.IsZero() {
		t.Error("claimed run has no StartedAt")
	}

	stored, err := s.Runs().Get(ctx, in.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if stored.Status != scenario.StatusRunning {
		t.Errorf("the store still records status %q; Claim did not persist the move to running",
			stored.Status)
	}
}

// Claim orders by QueuedAt, not by insertion order. A run created after
// another but backfilled with an earlier QueuedAt must still be claimed
// first — the same order the Fabriq store asks Postgres for with
// `ORDER BY queued_at ASC, id ASC`. A Claim that instead scanned insertion
// order would return the 10:00 run here, disagreeing with the database-backed
// store about which run is "oldest".
func TestClaimOrdersByQueuedAtNotInsertionOrder(t *testing.T) {
	t.Parallel()

	s := memory.New()
	ctx := context.Background()

	scenarioID := scnid.NewScenario()

	later := scenario.Run{
		ID:         scnid.NewRun(),
		ScenarioID: scenarioID,
		Manifest:   validManifest(),
		Status:     scenario.StatusQueued,
		QueuedAt:   time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC),
	}

	if _, err := s.Runs().Create(ctx, later); err != nil {
		t.Fatalf("Create later: %v", err)
	}

	// Created second, but backfilled with an earlier QueuedAt than the run
	// above — the case a pure insertion-order scan gets wrong.
	earlier := scenario.Run{
		ID:         scnid.NewRun(),
		ScenarioID: scenarioID,
		Manifest:   validManifest(),
		Status:     scenario.StatusQueued,
		QueuedAt:   time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC),
	}

	if _, err := s.Runs().Create(ctx, earlier); err != nil {
		t.Fatalf("Create earlier: %v", err)
	}

	claimed, err := s.Runs().Claim(ctx)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}

	if claimed.ID != earlier.ID {
		t.Fatalf("Claim returned run %s (queued at %v), want the 09:00 run %s",
			claimed.ID, claimed.QueuedAt, earlier.ID)
	}
}

// Claiming when nothing is queued — because the store is empty, or because
// every run present is already running or finished — returns a not-found
// error rather than a zero-valued Run. A zero Run has an empty Status, which
// would flow into a worker indistinguishable from a real claim.
func TestClaimWithNothingQueuedReturnsNotFound(t *testing.T) {
	t.Parallel()

	s := memory.New()
	ctx := context.Background()

	if _, err := s.Runs().Claim(ctx); !errors.Is(err, bserr.ErrNotFound) {
		t.Fatalf("Claim on an empty store: got err=%v, want ErrNotFound", err)
	}

	done := scenario.Run{
		ID:           scnid.NewRun(),
		ScenarioID:   scnid.NewScenario(),
		Manifest:     validManifest(),
		Status:       scenario.StatusComplete,
		SweepRunID:   scnid.NewRun(),
		CascadeRunID: scnid.NewRun(),
		QueuedAt:     time.Now().UTC(),
	}

	if _, err := s.Runs().Create(ctx, done); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := s.Runs().Claim(ctx); !errors.Is(err, bserr.ErrNotFound) {
		t.Fatalf("Claim with only a complete run present: got err=%v, want ErrNotFound", err)
	}
}

// This is the test that has to be genuinely concurrent. A version of Claim
// that reads the map to find the oldest queued run, releases the lock, and
// only then re-locks to write the claimed Status back would pass any
// sequential test — call Claim, then call it again — because a sequential
// caller never has a second goroutine inside the gap between the read and
// the write. Real goroutines racing against real logic do have something in
// that gap: two of them can both see the same run as queued before either
// one's write lands, and both walk away believing they claimed it.
//
// Running this under -race also catches the cruder bug: a Claim that reads
// or writes runOrder or the runs map without holding the mutex at all, which
// -race reports as a data race independent of whether this test's own
// duplicate check happens to notice it.
func TestClaimIsAtomicUnderConcurrency(t *testing.T) {
	t.Parallel()

	s := memory.New()
	ctx := context.Background()

	const workers = 20

	base := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	queued := make(map[string]bool, workers)

	for i := range workers {
		run := scenario.Run{
			ID:         scnid.NewRun(),
			ScenarioID: scnid.NewScenario(),
			Manifest:   validManifest(),
			Status:     scenario.StatusQueued,
			QueuedAt:   base.Add(time.Duration(i) * time.Second),
		}

		if _, err := s.Runs().Create(ctx, run); err != nil {
			t.Fatalf("Create run %d: %v", i, err)
		}

		queued[run.ID.String()] = true
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		claimed = make(map[string]int, workers)
	)

	for range workers {
		wg.Go(func() {
			got, err := s.Runs().Claim(ctx)
			if err != nil {
				// t.Errorf, unlike t.Fatalf, is safe to call from a
				// goroutine other than the test's own.
				t.Errorf("Claim: %v", err)
				return
			}

			mu.Lock()
			claimed[got.ID.String()]++
			mu.Unlock()
		})
	}

	wg.Wait()

	if len(claimed) != workers {
		t.Fatalf("%d distinct runs were claimed across %d concurrent workers; "+
			"want %d — a racy Claim let two workers see and take the same run",
			len(claimed), workers, workers)
	}

	for runID, count := range claimed {
		if count != 1 {
			t.Errorf("run %s was claimed %d times; Claim must give one run to one worker",
				runID, count)
		}

		if !queued[runID] {
			t.Errorf("run %s was claimed but was never one of the queued runs", runID)
		}
	}

	// Every run had exactly one worker and every worker got exactly one run,
	// so nothing should be left queued.
	if _, err := s.Runs().Claim(ctx); !errors.Is(err, bserr.ErrNotFound) {
		t.Errorf("Claim after every run was taken: got err=%v, want ErrNotFound", err)
	}
}
