package scenario

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/xraph/blackstart/extensions/geo/asset"
	"github.com/xraph/blackstart/pkg/id"
	solverv1 "github.com/xraph/blackstart/proto/gen/go/blackstart/solver/v1"
)

func oneBranchModel(volts float64) *solverv1.NetworkModel {
	return &solverv1.NetworkModel{
		Name:     "one branch",
		SBaseVa:  100e6,
		VBaseV:   volts,
		Branches: []*solverv1.Branch{aBranch(lines[0])},
	}
}

func fiveBranchModel(volts float64) *solverv1.NetworkModel {
	branches := make([]*solverv1.Branch, 0, len(lines))
	for _, line := range lines {
		branches = append(branches, aBranch(line))
	}

	return &solverv1.NetworkModel{
		Name:     "five branches",
		SBaseVa:  100e6,
		VBaseV:   volts,
		Branches: branches,
	}
}

// The static rating is deliberately non-zero: a branch the weather leaves
// alone keeps this, and a fixture that started at zero could not tell "kept
// its static rating" from "was rated at nothing".
func aBranch(line id.ID) *solverv1.Branch {
	return &solverv1.Branch{
		Id: line.String(), FromBus: 0, ToBus: 1,
		ROhm: 1, XOhm: 10, RatingVa: 500e6, InService: true, Tap: 1,
	}
}

// A branch whose chain breaks keeps its static rating and emits no override.
// A zero rating would make the branch infinitely overloaded and trip the whole
// network on the first screen, and one incomplete asset record is not a reason
// to abandon the scenario.
func TestAChainThatBreaksLeavesTheBranchStatic(t *testing.T) {
	t.Parallel()

	got, err := rate(
		t.Context(), oneBranchModel(345_000),
		&fakeGeo{routeErr: errNotFound}, &fakeHazard{ampacity: 900},
		issued, valid,
	)
	if err != nil {
		t.Fatalf("a broken chain is a gap in the asset record, not a run failure: %v", err)
	}

	if len(got.Overrides) != 0 {
		t.Errorf("got %d overrides, want none — the branch keeps its static rating",
			len(got.Overrides))
	}

	if got.WeatherRated != 0 {
		t.Errorf("WeatherRated = %d, want 0", got.WeatherRated)
	}

	if got.BranchCount != 1 {
		t.Errorf("BranchCount = %d, want 1 — the branch still exists", got.BranchCount)
	}
}

// The counts tell a reader how much of the network the weather actually
// touched. Three branches rated out of two thousand must not look like two
// thousand, which is what a reader assumes when only a rating list is shown.
func TestTheWeatherRatedCountReflectsReality(t *testing.T) {
	t.Parallel()

	// Five branches; only branches 1 and 3 have a route with rated spans.
	geo := &fakeGeo{
		routesByBranch: map[string]asset.Route{
			lines[1].String(): aRoute, lines[3].String(): aRoute,
		},
		spans: threeSpans,
	}
	haz := &fakeHazard{ampacity: 900}

	got, err := rate(t.Context(), fiveBranchModel(345_000), geo, haz, issued, valid)
	if err != nil {
		t.Fatalf("rate: %v", err)
	}

	if got.WeatherRated != 2 {
		t.Errorf("WeatherRated = %d, want 2", got.WeatherRated)
	}

	if got.BranchCount != 5 {
		t.Errorf("BranchCount = %d, want 5", got.BranchCount)
	}

	if len(got.Overrides) != 2 {
		t.Fatalf("got %d overrides, want 2 — one per rated branch", len(got.Overrides))
	}

	// The rating on the emit site itself, which is the only path that ships.
	// rateOneBranch exercises weakestSpan and voltAmperes side by side; it
	// cannot see rate wiring the two together wrongly, and putting raw
	// amperes in this field would hand the solver a branch reading as a
	// hundred thousand times overloaded.
	//
	// Spelled with the stdlib's sqrt while the implementation uses
	// pkg/quantity's, so this asserts a value it did not compute the same way.
	wantVA := math.Sqrt(3) * 345_000 * 900
	if rated := got.Overrides[0].GetBranchRating().GetRatingVa(); math.Abs(rated-wantVA) > 1 {
		t.Errorf("rating = %v VA, want %v — %v A unconverted would be the bug", rated,
			wantVA, 900.0)
	}

	// One read per rated branch. Three spans each, so a per-span reader would
	// show six.
	if haz.calls != 2 {
		t.Errorf("%d ampacity reads for 2 rated branches, want 2", haz.calls)
	}

	// The override names the branch by index into this same model's branch
	// list. Rating the right number of branches while naming the wrong ones
	// would move a storm onto lines it never touched, and every count above
	// would still be correct.
	for position, wantIndex := range []uint32{1, 3} {
		if named := got.Overrides[position].GetBranchRating().GetBranch(); named != wantIndex {
			t.Errorf("override %d names branch %d, want %d", position, named, wantIndex)
		}
	}
}

// An out-of-service branch is rated too. It stays in the model so that a
// contingency can restore it, and one restored mid-screen needs the weather's
// limit as much as any other — skipping it would leave exactly the branches a
// scenario is most likely to re-energise on their nameplate rating.
func TestAnOutOfServiceBranchIsStillRated(t *testing.T) {
	t.Parallel()

	model := oneBranchModel(345_000)
	model.GetBranches()[0].InService = false

	got, err := rate(
		t.Context(), model,
		&fakeGeo{route: aRoute, spans: threeSpans}, &fakeHazard{ampacity: 900},
		issued, valid,
	)
	if err != nil {
		t.Fatalf("rate: %v", err)
	}

	if got.WeatherRated != 1 || len(got.Overrides) != 1 {
		t.Errorf("WeatherRated = %d with %d overrides, want 1 and 1",
			got.WeatherRated, len(got.Overrides))
	}
}

// A model with no usable voltage base is refused outright, unlike a branch
// with no geometry. Every conversion would produce zero or a negative rating,
// so there is no partial answer worth returning — and unlike a missing route,
// this is the model being wrong rather than the asset record being thin.
func TestAModelWithNoVoltageBaseIsRefused(t *testing.T) {
	t.Parallel()

	model := oneBranchModel(0)

	if _, err := rate(
		t.Context(), model,
		&fakeGeo{route: aRoute, spans: threeSpans}, &fakeHazard{ampacity: 900},
		issued, valid,
	); err == nil {
		t.Error("rate accepted a model with no voltage base")
	}
}

// The failure the counts cannot describe.
//
// Five branches and a connection pool that gives out after two. Read as five
// lines of which two had geometry, this returns success with WeatherRated 2
// of 5 and the scenario is recorded as weather-bound with three lines quietly
// on nameplate. §4.2's "rated nothing" check does not fire, because two is not
// nothing — and the same pinned cycle answers differently on the next attempt.
func TestAFailurePartwayThroughFailsTheRunRatherThanTheCoverage(t *testing.T) {
	t.Parallel()

	geo := &fakeGeo{route: aRoute, spans: threeSpans, breakAfter: 2}

	got, err := rate(
		t.Context(), fiveBranchModel(345_000), geo, &fakeHazard{ampacity: 900},
		issued, valid,
	)
	if !errors.Is(err, errBroken) {
		t.Fatalf("rate returned WeatherRated %d of %d with err %v; want the transport error",
			got.WeatherRated, got.BranchCount, err)
	}
}

// A canceled run fails rather than reporting how far it got. Nothing
// downstream can tell a Rated built from a truncated walk from one built from
// a whole network, so the walk must not produce it.
func TestACancelledRunIsNotAPartialRating(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	got, err := rate(
		ctx, fiveBranchModel(345_000),
		&fakeGeo{route: aRoute, spans: threeSpans}, &fakeHazard{ampacity: 900},
		issued, valid,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("rate returned WeatherRated %d with err %v; want context.Canceled",
			got.WeatherRated, err)
	}
}
