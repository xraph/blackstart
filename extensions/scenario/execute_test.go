package scenario

import (
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/networkmodel/canonical"
	nmid "github.com/xraph/blackstart/extensions/networkmodel/id"
	nmmodel "github.com/xraph/blackstart/extensions/networkmodel/model"
	scn "github.com/xraph/blackstart/extensions/scenario/scenario"
	scnstore "github.com/xraph/blackstart/extensions/scenario/store"
	"github.com/xraph/blackstart/pkg/id"
)

// executing enqueues a run against a resolvable manifest, claims it, and
// executes it — the path a worker takes, so that Execute is never handed a
// run the queue did not hand out.
func executing(t *testing.T, f *fakes) (scn.ScenarioRun, scnstore.Store) {
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

// The manifest's pinned hash reaches both children.
//
// This is the join the whole hash-vocabulary change exists to make possible.
// A scenario navigates to its sweep and cascade by run id, so nothing was
// broken at runtime — but an operator holding the manifest's hash could not
// ask contingency or cascade "what else has been run against this network",
// because those records named it only in the solver's protobuf space and the
// manifest pins the canonical one.
//
// The assertion is on the REQUESTS rather than on the stored runs, because the
// fakes stand in for the two engines: what this extension is responsible for is
// handing the hash over, and contingency's and cascade's own tests cover what
// each does with it.
//
// Deleting `CanonicalHash: m.ModelHash` from screen.go fails the first half;
// deleting it from execute.go's cascade.Request fails the second. Both were
// run, and each fails with the empty string.
func TestBothChildRequestsCarryTheManifestsPinnedHash(t *testing.T) {
	t.Parallel()

	f := working()
	executed, _ := executing(t, f)

	want := executed.Manifest.ModelHash
	if want == "" {
		t.Fatal("the fixture's manifest pins no model, so this test proves nothing")
	}

	if got := f.screener.request.CanonicalHash; got != want {
		t.Errorf("the screen was told canonical hash %q, want the manifest's %q", got, want)
	}

	if got := f.simulator.request.CanonicalHash; got != want {
		t.Errorf("the cascade was told canonical hash %q, want the manifest's %q", got, want)
	}
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

// Whether a rating rests on a substituted voltage is a fact about the run,
// and it only becomes one by reaching the record.
//
// The fixture's buses state no nominal voltage of their own, so every rated
// branch converts against the model-level base — a rating that is right for
// the branches sitting at that level and out by the ratio of the two voltages
// for the rest. Nothing downstream can see that, because volt-amperes
// computed against the wrong voltage look exactly like volt-amperes. This
// count is the only evidence, and a count that stops at the internal Rated
// never reaches anybody.
func TestTheVoltageBaseFallbackReachesTheRecord(t *testing.T) {
	t.Parallel()

	f := working()
	executed, store := executing(t, f)

	stored, err := store.Runs().Get(t.Context(), executed.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}

	if stored.BaseVoltageFallback == nil {
		t.Fatal("base voltage fallback is nil after a run that rated every branch " +
			"against a voltage no bus stated")
	}

	if *stored.BaseVoltageFallback != len(lines) {
		t.Errorf("base voltage fallback = %d, want %d — every branch of this "+
			"fixture falls back", *stored.BaseVoltageFallback, len(lines))
	}
}

// A model that states its buses' voltages records a fallback of zero, not an
// absent one.
//
// The two are different findings and this is the boundary that would collapse
// them: nil says nobody counted, 0 says every rated branch converted against
// a voltage its own bus stated. A pipeline that only wrote the field when it
// had something to report would make the well-formed model indistinguishable
// from the unexamined one.
func TestAModelThatStatesItsVoltagesRecordsAFallbackOfZero(t *testing.T) {
	t.Parallel()

	f := working()
	e, store := engineWith(t, f)

	defined, err := e.Define(t.Context(), scn.Scenario{
		Name: "every voltage stated", Manifest: executableWithStatedVoltages(t, f),
	})
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

	stored, err := store.Runs().Get(t.Context(), executed.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}

	if stored.WeatherRated == nil || *stored.WeatherRated != len(lines) {
		t.Fatalf("weather rated = %v, want %d; the fallback count means nothing "+
			"without the rating it counts against", stored.WeatherRated, len(lines))
	}

	if stored.BaseVoltageFallback == nil {
		t.Fatal("base voltage fallback is nil; a model that fell back nowhere " +
			"must record 0, or it reads as one nobody examined")
	}

	if *stored.BaseVoltageFallback != 0 {
		t.Errorf("base voltage fallback = %d, want 0; every bus in this fixture "+
			"states its own nominal voltage", *stored.BaseVoltageFallback)
	}
}

// executableWithStatedVoltages is executable over a model whose every bus
// states the voltage it sits at, so no branch has to fall back.
func executableWithStatedVoltages(t *testing.T, f *fakes) scn.Manifest {
	t.Helper()

	model := aCanonicalModel()
	for index := range model.Buses {
		model.Buses[index].VNominalV = 345_000
	}

	return pinned(t, f, model)
}

// executableWithBranchOpen is executable over a model whose branch at index
// is out of service — the state rate.go deliberately keeps and rates, and the
// one an explicit policy is free to name.
func executableWithBranchOpen(t *testing.T, f *fakes, index int) scn.Manifest {
	t.Helper()

	model := aCanonicalModel()
	model.Branches[index].InService = false

	return pinned(t, f, model)
}

// pinned stores a model against the model fakes and returns the manifest that
// pins it by content hash.
//
// The hash is taken from the model each caller actually built rather than
// from aCanonicalModel: a manifest pinning one model while the fakes hold
// another resolves to a drift error, which is a test failure with nothing to
// do with what the test was asking about.
func pinned(t *testing.T, f *fakes, model canonical.Model) scn.Manifest {
	t.Helper()

	encoded, err := canonical.Encode(model)
	if err != nil {
		t.Fatalf("encoding the fixture: %v", err)
	}

	hash, err := canonical.Hash(model)
	if err != nil {
		t.Fatalf("hashing the fixture: %v", err)
	}

	f.models.found = nmmodel.NetworkModel{
		ID:          nmid.NewModel(),
		ContentHash: hash,
		Name:        theModelName,
		BusCount:    len(model.Buses),
		BranchCount: len(model.Branches),
	}
	f.models.encoded = encoded

	manifest := validManifest()
	manifest.ModelHash = hash

	return manifest
}

// The whole pipeline, against the case the review found: an explicit policy
// naming one branch the pinned model already has open alongside two real
// contingencies.
//
// The screen answers not-applicable for the open one and Ranked puts that
// answer first, so a run that took the front of the ranking would open an
// already-open branch, settle, complete, and link a cascade in which nothing
// happened — while the two contingencies the manifest asked about were never
// simulated. The assertion is on what the cascade was told to initiate,
// because that is the only place the difference shows.
func TestACascadeNeverInitiatesOnAnAlreadyOpenBranch(t *testing.T) {
	t.Parallel()

	f := working()
	f.screener.run = aSweepOverAnOpenBranch()

	e, store := engineWith(t, f)

	manifest := executableWithBranchOpen(t, f, 4)
	manifest.Policy = scn.PolicyExplicit
	manifest.Branches = []string{
		lines[4].String(), lines[1].String(), lines[0].String(),
	}

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

	if executed.Status != scn.StatusComplete {
		t.Fatalf("status = %q (%+v), want complete", executed.Status, executed.Failure)
	}

	initiating := f.simulator.request.Initiating
	if len(initiating) != 1 || initiating[0] != 1 {
		t.Errorf("the cascade initiated on %v, want branch 1 — branch 4 is out of "+
			"service in the pinned model, and opening it again is a cascade in "+
			"which nothing happens presented as this scenario's answer", initiating)
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

// A stale copy of a claimed run does not execute.
//
// The argument is whatever a caller has been holding, and a run finished
// elsewhere — or requeued after a timeout — still reads "running" in a
// variable captured before that happened. Finish would refuse the write at
// the end, but only after a sweep and a cascade had been recorded by two
// other extensions with nothing pointing at them.
func TestExecuteRefusesAStaleClaimedRun(t *testing.T) {
	t.Parallel()

	f := working()
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

	// Somebody else finished it. The caller's copy still says running.
	finished := claimed
	finished.Status = scn.StatusFailed
	finished.Failure = &scn.Failure{Step: scn.StepCascade, Reason: "the worker was reaped"}
	finished.FinishedAt = issued

	if _, err := store.Runs().Finish(t.Context(), finished); err != nil {
		t.Fatalf("finish: %v", err)
	}

	if _, err := e.Execute(t.Context(), claimed); err == nil {
		t.Fatal("want an error: the stored run is no longer running")
	}

	if f.screener.calls != 0 || f.simulator.calls != 0 {
		t.Errorf("a stale run still ran the pipeline: %d screens, %d cascades — "+
			"both would have left an orphan child record", f.screener.calls,
			f.simulator.calls)
	}

	stored, err := store.Runs().Get(t.Context(), claimed.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}

	if stored.Failure == nil || stored.Failure.Reason != "the worker was reaped" {
		t.Errorf("the first outcome was overwritten: %+v", stored.Failure)
	}
}
