package scenario

import (
	"strings"
	"testing"

	cid "github.com/xraph/blackstart/extensions/cascade/id"
	cascaderun "github.com/xraph/blackstart/extensions/cascade/run"
	crid "github.com/xraph/blackstart/extensions/contingency/id"
	"github.com/xraph/blackstart/extensions/contingency/sweep"
)

// aSweep is a screen result whose worst candidate is branch 3: a radial
// outage, which Ranked sorts ahead of every evaluated one because it is
// unknown rather than safe. Branch 1 is the worst EVALUATED one, so a
// pipeline that ranked by loading alone would pick it and this fixture would
// notice.
func aSweep() sweep.Run {
	loading := func(v float64) *float64 { return &v }

	return sweep.Run{
		ID:            crid.NewRun(),
		ModelHash:     "sha256:screened",
		SolverVersion: "0.0.0",
		Candidates: []sweep.Candidate{
			{
				ContingencyID: lines[0].String(), Branch: 0,
				Screenability: sweep.Screened, WorstLoading: loading(0.4),
			},
			{
				ContingencyID: lines[1].String(), Branch: 1,
				Screenability: sweep.Screened, WorstLoading: loading(1.8),
			},
			{
				ContingencyID: lines[3].String(), Branch: 3,
				Screenability: sweep.Radial,
			},
		},
	}
}

// aSweepOverAnOpenBranch is what the screen returns for an explicit policy
// naming branch 4, which the pinned model already has out of service: the
// sweep answers not-applicable for it, and Ranked puts that answer in the
// same leading bucket as a radial outage. Branches 1 and 0 are the real
// contingencies the manifest also named.
func aSweepOverAnOpenBranch() sweep.Run {
	loading := func(v float64) *float64 { return &v }

	return sweep.Run{
		ID:            crid.NewRun(),
		ModelHash:     "sha256:screened",
		SolverVersion: "0.0.0",
		Candidates: []sweep.Candidate{
			{
				ContingencyID: lines[4].String(), Branch: 4,
				Screenability: sweep.NotApplicable,
			},
			{
				ContingencyID: lines[1].String(), Branch: 1,
				Screenability: sweep.Screened, WorstLoading: loading(1.8),
			},
			{
				ContingencyID: lines[0].String(), Branch: 0,
				Screenability: sweep.Screened, WorstLoading: loading(0.4),
			},
		},
	}
}

func aCascade() cascaderun.CascadeRun {
	return cascaderun.CascadeRun{
		ID:            cid.NewRun(),
		SolverVersion: "0.0.0",
		Outcome:       cascaderun.OutcomeSettled,
	}
}

// working is a collaborator set whose whole pipeline succeeds.
func working() *fakes {
	f := newFakes()
	f.screener.run = aSweep()
	f.simulator.run = aCascade()

	return f
}

// The worst-ranked candidate is the one the cascade will initiate from, and
// ranking is the sweep's own: an outage the screen could not evaluate sorts
// ahead of every evaluated one, because it is unknown rather than safe and is
// usually the most consequential thing on the list.
func TestTheWorstRankedCandidateIsTheRadialOne(t *testing.T) {
	t.Parallel()

	f := working()
	e, _ := engineWith(t, f)

	screened, worst, err := e.screen(
		t.Context(), validManifest(), fiveBranchModel(345_000), nil)
	if err != nil {
		t.Fatalf("screen: %v", err)
	}

	if screened.ID != f.screener.run.ID {
		t.Errorf("returned sweep %s, want %s", screened.ID, f.screener.run.ID)
	}

	if worst.Branch != 3 {
		t.Errorf("worst candidate is branch %d, want 3 — the radial outage ranks "+
			"ahead of the 180%%-loaded evaluated one", worst.Branch)
	}
}

// A screen that evaluated nothing has no worst outage, and a scenario cannot
// invent one: cascading branch zero would be an answer to a question the
// screen never asked.
func TestASweepWithNoCandidatesHasNoWorstOutage(t *testing.T) {
	t.Parallel()

	f := working()
	empty := aSweep()
	empty.Candidates = nil
	f.screener.run = empty

	e, _ := engineWith(t, f)

	if _, _, err := e.screen(
		t.Context(), validManifest(), fiveBranchModel(345_000), nil,
	); err == nil {
		t.Fatal("want an error: no candidate to cascade")
	}
}

// A branch that is already open is not the worst outage. Its outage is not an
// outage: cascading it would open an open branch, settle with nothing having
// happened, and present that as the scenario's answer.
//
// It ranks first all the same — Ranked's leading bucket is everything the
// screen could not evaluate, and not-applicable shares it with radial — so
// nothing but this predicate stands between the fixture and a false
// all-clear.
func TestAnAlreadyOpenBranchIsNotTheWorstOutage(t *testing.T) {
	t.Parallel()

	f := working()
	f.screener.run = aSweepOverAnOpenBranch()

	e, _ := engineWith(t, f)

	_, worst, err := e.screen(
		t.Context(), validManifest(), fiveBranchModel(345_000), nil)
	if err != nil {
		t.Fatalf("screen: %v", err)
	}

	if worst.Branch != 1 {
		t.Errorf("worst candidate is branch %d, want 1 — branch 4 is already out "+
			"of service, so its outage is not a contingency", worst.Branch)
	}
}

// If passing over the already-open branches empties the list there is no
// outage to cascade, and the run has to say so rather than reach for the
// front of the ranking anyway.
func TestASweepOfNothingButOpenBranchesHasNoWorstOutage(t *testing.T) {
	t.Parallel()

	f := working()

	open := aSweepOverAnOpenBranch()
	open.Candidates = open.Candidates[:1]
	f.screener.run = open

	e, _ := engineWith(t, f)

	_, _, err := e.screen(t.Context(), validManifest(), fiveBranchModel(345_000), nil)
	if err == nil {
		t.Fatal("want an error: every candidate names a branch that is already open")
	}

	if !strings.Contains(err.Error(), "out of service") {
		t.Errorf("the refusal does not say why there is no outage: %v", err)
	}
}

// The overrides reach the screen, so the ranking describes the rated network
// rather than the nameplate one.
func TestTheScreenIsGivenTheOverrides(t *testing.T) {
	t.Parallel()

	f := working()
	e, _ := engineWith(t, f)

	rated, err := rate(
		t.Context(), fiveBranchModel(345_000), f.geo, f.hazard, issued, valid)
	if err != nil {
		t.Fatalf("rate: %v", err)
	}

	if _, _, err := e.screen(
		t.Context(), validManifest(), fiveBranchModel(345_000), rated.Overrides,
	); err != nil {
		t.Fatalf("screen: %v", err)
	}

	if len(f.screener.request.Overrides) != len(rated.Overrides) {
		t.Errorf("the screen got %d overrides, want %d",
			len(f.screener.request.Overrides), len(rated.Overrides))
	}
}
