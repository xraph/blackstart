package memory_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
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

	in := scenario.ScenarioRun{
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
		run := scenario.ScenarioRun{
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
	other := scenario.ScenarioRun{
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

// Listing across scenarios returns every run, whatever scenario it belongs
// to. ListByScenario's filter is the thing this method drops, so a List that
// still filtered — to the last scenario written, say — would answer a console
// asking "what has run lately" with one scenario's history.
func TestListReturnsRunsFromEveryScenario(t *testing.T) {
	t.Parallel()

	s := memory.New()
	ctx := context.Background()

	base := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	want := make(map[string]bool, 4)

	for i := range 4 {
		run := scenario.ScenarioRun{
			ID: scnid.NewRun(),
			// A distinct scenario per run: nothing here shares a parent, so a
			// listing that filtered by any one of them returns a single row.
			ScenarioID: scnid.NewScenario(),
			Manifest:   validManifest(),
			Status:     scenario.StatusQueued,
			QueuedAt:   base.Add(time.Duration(i) * time.Hour),
		}

		if _, err := s.Runs().Create(ctx, run); err != nil {
			t.Fatalf("Create run %d: %v", i, err)
		}

		want[run.ID.String()] = true
	}

	got, err := s.Runs().List(ctx, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("List returned %d runs, want all %d across four scenarios", len(got), len(want))
	}

	for _, run := range got {
		if !want[run.ID.String()] {
			t.Errorf("List returned run %s, which was never created", run.ID)
		}

		delete(want, run.ID.String())
	}
}

// The ordering test, and the one this primitive was most likely to get wrong.
//
// Runs are created in an order that is not their QueuedAt order, so a List
// that walked runOrder — or reversed it — returns something different from a
// List that sorts. Both stores are held to `queued_at DESC, id DESC`; the
// Fabriq half of this fixture is TestListOrdersRunsAcrossScenariosNewestFirst
// in the integration tests, built from the same out-of-order insertion so the
// two answers can be compared to the same expectation rather than to each
// other's implementation.
func TestListOrdersByQueuedAtNotInsertionOrder(t *testing.T) {
	t.Parallel()

	s := memory.New()
	ctx := context.Background()

	base := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)

	// Hours 2, 0, 3, 1 — every adjacent pair is out of order in one direction
	// or the other, so neither insertion order nor its reverse is the answer.
	hours := []int{2, 0, 3, 1}

	byHour := make(map[int]string, len(hours))

	for _, hour := range hours {
		run := scenario.ScenarioRun{
			ID:         scnid.NewRun(),
			ScenarioID: scnid.NewScenario(),
			Manifest:   validManifest(),
			Status:     scenario.StatusQueued,
			QueuedAt:   base.Add(time.Duration(hour) * time.Hour),
		}

		if _, err := s.Runs().Create(ctx, run); err != nil {
			t.Fatalf("Create the %d-hour run: %v", hour, err)
		}

		byHour[hour] = run.ID.String()
	}

	got, err := s.Runs().List(ctx, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(got) != len(hours) {
		t.Fatalf("List returned %d runs, want %d", len(got), len(hours))
	}

	// Newest first: hours 3, 2, 1, 0.
	for index, hour := range []int{3, 2, 1, 0} {
		if got[index].ID.String() != byHour[hour] {
			t.Errorf("position %d holds the run queued at %v, want the %d-hour one; "+
				"List is not ordering by queued_at descending",
				index, got[index].QueuedAt, hour)
		}
	}
}

// Two runs queued in the same instant order by identifier descending, so the
// list is reproducible rather than dependent on which was written first. The
// Fabriq store's `ORDER BY queued_at DESC, id DESC` breaks the tie the same
// way; without a tie-break here the two stores answer differently whenever a
// pair shares a timestamp.
func TestListBreaksTiesOnIdentifierDescending(t *testing.T) {
	t.Parallel()

	s := memory.New()
	ctx := context.Background()

	instant := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)

	var ids []string

	for range 5 {
		run := scenario.ScenarioRun{
			ID:         scnid.NewRun(),
			ScenarioID: scnid.NewScenario(),
			Manifest:   validManifest(),
			Status:     scenario.StatusQueued,
			QueuedAt:   instant,
		}

		if _, err := s.Runs().Create(ctx, run); err != nil {
			t.Fatalf("Create: %v", err)
		}

		ids = append(ids, run.ID.String())
	}

	got, err := s.Runs().List(ctx, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	slices.Sort(ids)
	slices.Reverse(ids)

	for index, want := range ids {
		if got[index].ID.String() != want {
			t.Fatalf("position %d holds %s, want %s — runs queued in one instant "+
				"must order by identifier descending", index, got[index].ID, want)
		}
	}
}

// A limit of zero or less returns everything, and a positive one returns that
// many of the newest. scenario.RunStore documents the first half and the HTTP
// surface depends on the second; a store that clamped a zero to some default
// would break a CLI, and one that ignored a positive limit would let one
// request page the whole table.
func TestListHonoursTheLimitAndTreatsZeroAsEverything(t *testing.T) {
	t.Parallel()

	s := memory.New()
	ctx := context.Background()

	base := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)

	// More than pkgstore's default page of 100, so a reintroduced clamp to it
	// is what this catches rather than something a smaller fixture would hide.
	const runCount = 120

	newest := ""

	for i := range runCount {
		run := scenario.ScenarioRun{
			ID:         scnid.NewRun(),
			ScenarioID: scnid.NewScenario(),
			Manifest:   validManifest(),
			Status:     scenario.StatusQueued,
			QueuedAt:   base.Add(time.Duration(i) * time.Minute),
		}

		if _, err := s.Runs().Create(ctx, run); err != nil {
			t.Fatalf("Create run %d: %v", i, err)
		}

		newest = run.ID.String()
	}

	for _, limit := range []int{0, -1} {
		got, err := s.Runs().List(ctx, limit)
		if err != nil {
			t.Fatalf("List(%d): %v", limit, err)
		}

		if len(got) != runCount {
			t.Errorf("List(%d) returned %d runs, want all %d", limit, len(got), runCount)
		}
	}

	got, err := s.Runs().List(ctx, 3)
	if err != nil {
		t.Fatalf("List(3): %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("a limit of 3 returned %d rows", len(got))
	}

	// The newest three, not any three: a limit applied before the sort would
	// return the same count and the wrong rows.
	if got[0].ID.String() != newest {
		t.Errorf("the limited list starts at %s, want the newest run %s; "+
			"the limit must be applied after the ordering", got[0].ID, newest)
	}
}

// aClaimedRun stores a queued run and claims it, which is the only state
// Finish accepts.
func aClaimedRun(t *testing.T, s *memory.Store) scenario.ScenarioRun {
	t.Helper()

	in := scenario.ScenarioRun{
		ID:         scnid.NewRun(),
		ScenarioID: scnid.NewScenario(),
		Manifest:   validManifest(),
		Status:     scenario.StatusQueued,
		QueuedAt:   time.Now().UTC(),
	}

	if _, err := s.Runs().Create(context.Background(), in); err != nil {
		t.Fatalf("Create: %v", err)
	}

	claimed, err := s.Runs().Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}

	return claimed
}

// Finishing a claimed run records its terminal state, and the record is what
// a later Get returns — without this the run stays running forever and the
// answer it produced is nowhere.
func TestFinishRecordsATerminalState(t *testing.T) {
	t.Parallel()

	s := memory.New()
	ctx := context.Background()

	claimed := aClaimedRun(t, s)
	claimed.Status = scenario.StatusFailed
	claimed.Failure = &scenario.Failure{
		Step: scenario.StepRatings, Reason: "the cycle rated nothing",
	}
	claimed.FinishedAt = time.Now().UTC()

	if _, err := s.Runs().Finish(ctx, claimed); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	stored, err := s.Runs().Get(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if stored.Status != scenario.StatusFailed {
		t.Errorf("stored status is %q; Finish did not persist the terminal state",
			stored.Status)
	}

	if stored.Failure == nil || stored.Failure.Step != scenario.StepRatings {
		t.Errorf("stored failure is %+v, want the step it failed at", stored.Failure)
	}
}

// The coverage counts a finished run carries reach this store too, the
// fallback count among them.
//
// Parity, which is the thing this plan has already been burned on. The Fabriq
// store maps a run field by field onto columns and has a test per field for
// exactly that reason; this store keeps whole structs, so the field arrives
// for free — and "for free" is the state that stops being true the moment
// Finish starts carrying fields forward selectively, which it already does
// for the four frozen ones. A count of zero is asserted rather than some
// other number because zero is the value a store that silently dropped the
// field would also produce.
func TestFinishRecordsTheCoverageCountsIncludingTheVoltageBaseFallback(t *testing.T) {
	t.Parallel()

	s := memory.New()
	ctx := context.Background()

	rated, branches, fellBack := 1841, 2000, 0

	claimed := aClaimedRun(t, s)
	claimed.Status = scenario.StatusComplete
	claimed.SweepRunID = scnid.NewRun()
	claimed.CascadeRunID = scnid.NewRun()
	claimed.WeatherRated = &rated
	claimed.BranchCount = &branches
	claimed.BaseVoltageFallback = &fellBack
	claimed.FinishedAt = time.Now().UTC()

	if _, err := s.Runs().Finish(ctx, claimed); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	stored, err := s.Runs().Get(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if stored.WeatherRated == nil || *stored.WeatherRated != rated {
		t.Errorf("weather rated stored as %v, want %d", stored.WeatherRated, rated)
	}

	if stored.BaseVoltageFallback == nil {
		t.Fatal("base voltage fallback stored as nil; a run that fell back nowhere " +
			"must read as 0, not as one nobody counted")
	}

	if *stored.BaseVoltageFallback != fellBack {
		t.Errorf("base voltage fallback stored as %d, want %d",
			*stored.BaseVoltageFallback, fellBack)
	}
}

// A run that is not running cannot finish. A second worker that believed it
// owned a run someone else already finished must be told, not allowed to
// write its own answer over the first one's.
func TestFinishRefusesARunThatIsNotRunning(t *testing.T) {
	t.Parallel()

	s := memory.New()
	ctx := context.Background()

	claimed := aClaimedRun(t, s)
	claimed.Status = scenario.StatusComplete
	claimed.SweepRunID = scnid.NewRun()
	claimed.CascadeRunID = scnid.NewRun()
	claimed.FinishedAt = time.Now().UTC()

	if _, err := s.Runs().Finish(ctx, claimed); err != nil {
		t.Fatalf("first Finish: %v", err)
	}

	if _, err := s.Runs().Finish(ctx, claimed); !errors.Is(err, bserr.ErrConflict) {
		t.Fatalf("finishing an already-finished run returned %v, want a conflict", err)
	}
}

// Finish is not a way back into the queue. Writing a queued status over a run
// that already produced an answer would hand it to a second worker to execute
// again.
func TestFinishRefusesANonTerminalStatus(t *testing.T) {
	t.Parallel()

	s := memory.New()

	claimed := aClaimedRun(t, s)
	claimed.Status = scenario.StatusQueued

	if _, err := s.Runs().Finish(context.Background(), claimed); !errors.Is(err, bserr.ErrInvalid) {
		t.Fatalf("finishing as queued returned %v, want invalid", err)
	}
}

// Finishing a run nothing recorded is not found, not a quiet insert.
func TestFinishRefusesARunThatDoesNotExist(t *testing.T) {
	t.Parallel()

	s := memory.New()

	orphan := scenario.ScenarioRun{
		ID:         scnid.NewRun(),
		ScenarioID: scnid.NewScenario(),
		Manifest:   validManifest(),
		Status:     scenario.StatusFailed,
		Failure:    &scenario.Failure{Step: scenario.StepResolve, Reason: "no such model"},
		QueuedAt:   time.Now().UTC(),
	}

	if _, err := s.Runs().Finish(context.Background(), orphan); !errors.Is(err, bserr.ErrNotFound) {
		t.Fatalf("finishing an unknown run returned %v, want not-found", err)
	}
}

// Claiming a queued run returns it and moves it to running, and the move is
// visible to a later Get — Claim is not just handing back a mutated copy
// while leaving the stored record untouched.
func TestClaimTakesTheOldestQueuedRunAndMovesItToRunning(t *testing.T) {
	t.Parallel()

	s := memory.New()
	ctx := context.Background()

	in := scenario.ScenarioRun{
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

	later := scenario.ScenarioRun{
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
	earlier := scenario.ScenarioRun{
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

	done := scenario.ScenarioRun{
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
		run := scenario.ScenarioRun{
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

// Finish cannot move the frozen manifest.
//
// It writes a whole record from an argument a caller has been holding. Today
// that argument is always Claim's output, but a retry endpoint or a stuck-run
// reaper would hand back something else, and a manifest arriving that way
// would replace the frozen one — leaving a record that is internally
// consistent and still looks citable. Which scenario the run belongs to, when
// it was queued and when it was claimed are frozen for the same reason: none
// of the three is an outcome of executing.
//
// The claim time is the one a caller is most likely to get wrong by accident
// rather than by tampering — a reaper building a terminal run from the id and
// the outcome hands back a zero StartedAt, which is not a wrong time but no
// time at all.
func TestFinishCannotMoveTheFrozenManifest(t *testing.T) {
	t.Parallel()

	s := memory.New()
	ctx := context.Background()

	claimed := aClaimedRun(t, s)

	tampered := claimed
	tampered.Manifest.ModelHash = "sha256:a-different-network"
	tampered.Manifest.Seed = 999
	tampered.ScenarioID = scnid.NewScenario()
	tampered.QueuedAt = claimed.QueuedAt.Add(72 * time.Hour)
	tampered.StartedAt = time.Time{}
	tampered.Status = scenario.StatusComplete
	tampered.SweepRunID = scnid.NewRun()
	tampered.CascadeRunID = scnid.NewRun()
	tampered.FinishedAt = time.Now().UTC()

	if _, err := s.Runs().Finish(ctx, tampered); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	stored, err := s.Runs().Get(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if stored.Manifest.ModelHash != claimed.Manifest.ModelHash {
		t.Errorf("the stored manifest moved to %q, want the frozen %q",
			stored.Manifest.ModelHash, claimed.Manifest.ModelHash)
	}

	if stored.Manifest.Seed != claimed.Manifest.Seed {
		t.Errorf("the stored seed moved to %d", stored.Manifest.Seed)
	}

	if stored.ScenarioID != claimed.ScenarioID {
		t.Errorf("the run now belongs to scenario %s, want %s",
			stored.ScenarioID, claimed.ScenarioID)
	}

	if !stored.QueuedAt.Equal(claimed.QueuedAt) {
		t.Errorf("the queue time moved to %v", stored.QueuedAt)
	}

	if !stored.StartedAt.Equal(claimed.StartedAt) {
		t.Errorf("the claim time is now %v, want the stamped %v; a run that was "+
			"claimed and finished cannot have no start", stored.StartedAt, claimed.StartedAt)
	}

	// The outcome, by contrast, is exactly what Finish is for.
	if stored.Status != scenario.StatusComplete || stored.SweepRunID != tampered.SweepRunID {
		t.Errorf("Finish did not record the outcome: %+v", stored)
	}
}
