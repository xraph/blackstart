package scenario

import (
	"strings"
	"testing"

	scn "github.com/xraph/blackstart/extensions/scenario/scenario"
	scnstore "github.com/xraph/blackstart/extensions/scenario/store"
	"github.com/xraph/blackstart/pkg/id"
)

// executing enqueues a run against a resolvable manifest, claims it, and
// executes it — the path a worker takes, so that Execute is never handed a
// run the queue did not hand out.
func executing(t *testing.T, f *fakes) (scn.Run, scnstore.Store) {
	t.Helper()

	e, store := engineWith(t, f)
	manifest := executable(t, f)

	defined, err := e.Define(t.Context(), scn.Scenario{Name: "derecho", Manifest: manifest})
	if err != nil {
		t.Fatalf("define: %v", err)
	}

	if _, err := e.Enqueue(t.Context(), defined.ID); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	claimed, err := store.Runs().Claim(t.Context())
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	executed, err := e.Execute(t.Context(), claimed)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	return executed, store
}

// A completed run names both children and the solver that produced the
// answer. Without both ids nothing links the manifest that was requested to
// the numbers that came out of it.
func TestASuccessfulRunStoresBothChildIds(t *testing.T) {
	t.Parallel()

	f := working()
	executed, store := executing(t, f)

	stored, err := store.Runs().Get(t.Context(), executed.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}

	if stored.Status != scn.StatusComplete {
		t.Fatalf("status = %q (%+v), want complete", stored.Status, stored.Failure)
	}

	if stored.SweepRunID != f.screener.run.ID {
		t.Errorf("sweep run id = %s, want %s", stored.SweepRunID, f.screener.run.ID)
	}

	if stored.CascadeRunID != f.simulator.run.ID {
		t.Errorf("cascade run id = %s, want %s", stored.CascadeRunID, f.simulator.run.ID)
	}

	// Read off the cascade's own record rather than configured in, so the run
	// states what actually produced it.
	if stored.SolverVersion != f.simulator.run.SolverVersion {
		t.Errorf("solver version = %q, want %q", stored.SolverVersion,
			f.simulator.run.SolverVersion)
	}

	if stored.WeatherRated == nil || *stored.WeatherRated != len(lines) {
		t.Errorf("weather rated = %v, want %d", stored.WeatherRated, len(lines))
	}

	if stored.FinishedAt.IsZero() {
		t.Error("a complete run has no finish time")
	}
}

// The screen runs against the rated network, not the nameplate one, and so
// does the cascade — otherwise the ranking and the simulation would disagree
// about what the lines can carry, and the coupling this whole extension
// exists to demonstrate would not be in the answer.
func TestTheScreenAndTheCascadeBothGetTheRatings(t *testing.T) {
	t.Parallel()

	f := working()
	executing(t, f)

	if len(f.screener.request.Overrides) != len(lines) {
		t.Errorf("the screen got %d overrides for %d rated branches",
			len(f.screener.request.Overrides), len(lines))
	}

	if len(f.simulator.request.Overrides) != len(f.screener.request.Overrides) {
		t.Errorf("the cascade got %d overrides and the screen got %d; they must "+
			"simulate the same network", len(f.simulator.request.Overrides),
			len(f.screener.request.Overrides))
	}

	// The rating itself, not merely the count: an override carrying raw
	// amperes in a volt-ampere field would pass every count assertion.
	rating := f.screener.request.Overrides[0].GetBranchRating().GetRatingVa()
	if rating < 1e8 {
		t.Errorf("override rating = %v VA, which is ampere-scale; 900 A at 345 kV "+
			"is about 5.4e8 VA", rating)
	}
}

// The policy reaches the screen. All names nothing, which the contingency
// contract reads as every in-service branch; explicit names its own list, in
// its own order.
func TestThePolicyReachesTheScreen(t *testing.T) {
	t.Parallel()

	all := working()
	executing(t, all)

	if all.screener.request.Contingencies != nil {
		t.Errorf("policy all sent %d contingencies; the contract reads none as "+
			"every in-service branch", len(all.screener.request.Contingencies))
	}

	explicit := working()
	e, store := engineWith(t, explicit)

	manifest := executable(t, explicit)
	manifest.Policy = scn.PolicyExplicit
	manifest.Branches = []string{lines[2].String(), lines[0].String()}

	defined, err := e.Define(t.Context(), scn.Scenario{Name: "two lines", Manifest: manifest})
	if err != nil {
		t.Fatalf("define: %v", err)
	}

	if _, err := e.Enqueue(t.Context(), defined.ID); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	claimed, err := store.Runs().Claim(t.Context())
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	if _, err := e.Execute(t.Context(), claimed); err != nil {
		t.Fatalf("execute: %v", err)
	}

	sent := explicit.screener.request.Contingencies
	if len(sent) != 2 || sent[0].GetBranch() != 2 || sent[1].GetBranch() != 0 {
		t.Errorf("the screen got %+v, want branches 2 then 0 in the manifest's order", sent)
	}
}

// A cycle that rated nothing fails at the rating step, names the cycle, and
// produces no cascade. A cascade computed on nameplate ratings while naming a
// forecast is the most misleading result this platform could produce, because
// it looks like an answer.
func TestNoRatingsForTheCycleFailsAndProducesNoCascade(t *testing.T) {
	t.Parallel()

	f := working()
	// Every branch reaches its spans; the weather has computed nothing.
	f.hazard.bySpan = map[id.ID]float64{}

	executed, store := executing(t, f)

	stored, err := store.Runs().Get(t.Context(), executed.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}

	if stored.Status != scn.StatusFailed {
		t.Fatalf("status = %q, want failed", stored.Status)
	}

	if stored.Failure == nil || stored.Failure.Step != scn.StepRatings {
		t.Fatalf("failure = %+v, want one naming the rating step", stored.Failure)
	}

	if !strings.Contains(stored.Failure.Reason, theCycle.ID.String()) {
		t.Errorf("the failure does not name the cycle: %q", stored.Failure.Reason)
	}

	if f.simulator.calls != 0 || f.screener.calls != 0 {
		t.Errorf("the run continued past the rating gap: %d screens, %d cascades",
			f.screener.calls, f.simulator.calls)
	}

	// Rated zero of five is a finding and must be on the record. Nil here
	// would say the rating step never ran.
	if stored.WeatherRated == nil || *stored.WeatherRated != 0 {
		t.Errorf("weather rated = %v, want a pointer to 0", stored.WeatherRated)
	}

	if stored.BranchCount == nil || *stored.BranchCount != len(lines) {
		t.Errorf("branch count = %v, want %d", stored.BranchCount, len(lines))
	}
}

// A model whose branches never reach the asset record fails at the same step
// with a different message, because it is a different problem: the fix is in
// the model-to-geo join, and nothing about the forecast would help.
func TestAModelThatJoinsNothingSaysSoRatherThanBlamingTheCycle(t *testing.T) {
	t.Parallel()

	f := working()
	f.geo.routeErr = errNotFound

	executed, store := executing(t, f)

	stored, err := store.Runs().Get(t.Context(), executed.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}

	if stored.Failure == nil || stored.Failure.Step != scn.StepRatings {
		t.Fatalf("failure = %+v, want one naming the rating step", stored.Failure)
	}

	if !strings.Contains(stored.Failure.Reason, "asset record") {
		t.Errorf("the failure blames the forecast for a join problem: %q",
			stored.Failure.Reason)
	}
}

// Every stage records which stage it was. A run that failed without saying
// where is one nobody can act on, and the five steps fail for reasons that
// live in five different places.
func TestEachStageRecordsWhereItFailed(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		breaks func(*fakes)
		step   scn.Step
	}{
		{"resolve", func(f *fakes) { f.models.foundErr = errNotFound }, scn.StepResolve},
		{"ratings", func(f *fakes) { f.hazard.err = errBroken }, scn.StepRatings},
		{"screen", func(f *fakes) { f.screener.err = errBroken }, scn.StepScreen},
		{"cascade", func(f *fakes) { f.simulator.err = errBroken }, scn.StepCascade},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := working()
			tc.breaks(f)

			executed, store := executing(t, f)

			stored, err := store.Runs().Get(t.Context(), executed.ID)
			if err != nil {
				t.Fatalf("get run: %v", err)
			}

			if stored.Status != scn.StatusFailed {
				t.Fatalf("status = %q, want failed", stored.Status)
			}

			if stored.Failure == nil || stored.Failure.Step != tc.step {
				t.Fatalf("failure = %+v, want the %s step", stored.Failure, tc.step)
			}

			if stored.Failure.Reason == "" {
				t.Error("the failure names a step and no reason")
			}

			if stored.FinishedAt.IsZero() {
				t.Error("a failed run has no finish time")
			}
		})
	}
}

// Execute refuses a run the queue did not hand out. Claiming is what stops
// two workers from executing one scenario twice, and an Execute that accepted
// a queued run would let a caller go around it.
func TestExecuteRefusesARunThatWasNotClaimed(t *testing.T) {
	t.Parallel()

	f := working()
	e, store := engineWith(t, f)
	manifest := executable(t, f)

	defined, err := e.Define(t.Context(), scn.Scenario{Name: "derecho", Manifest: manifest})
	if err != nil {
		t.Fatalf("define: %v", err)
	}

	queued, err := e.Enqueue(t.Context(), defined.ID)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	if _, err := e.Execute(t.Context(), queued); err == nil {
		t.Fatal("want an error: the run was never claimed")
	}

	stored, err := store.Runs().Get(t.Context(), queued.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}

	if stored.Status != scn.StatusQueued {
		t.Errorf("the refused run is now %q; it must still be queued", stored.Status)
	}

	// Refused before any work, not after. The store's own Finish would also
	// refuse an unclaimed run, but only once the pipeline had already screened
	// and cascaded — leaving two child runs recorded by other extensions with
	// no scenario run pointing at them.
	if f.screener.calls != 0 || f.simulator.calls != 0 {
		t.Errorf("an unclaimed run still ran the pipeline: %d screens, %d cascades",
			f.screener.calls, f.simulator.calls)
	}
}
