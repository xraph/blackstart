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
// One incomplete asset record is not a reason to abandon the scenario, and an
// override of zero would be worse than none: every consumer reads a
// non-positive rating as unrated and skips the branch, so a zero would remove
// that line from every violation check rather than fail one.
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

// The two ways a model can come back rated nothing are counted apart.
//
// Both produce zero overrides and they need different fixes: one is a join
// between the model and the asset record — solverv1.Branch.id is a free
// string while geo keys on typed platform identifiers, so a model imported
// without them rates nothing for a reason that has nothing to do with weather
// — and the other is a pinned cycle that computed nothing. A caller with only
// WeatherRated to look at cannot tell which it is holding.
func TestLocatedTellsAnUnjoinedModelFromAnUnratedCycle(t *testing.T) {
	t.Parallel()

	unjoined, err := rate(
		t.Context(), fiveBranchModel(345_000),
		&fakeGeo{routeErr: errNotFound}, &fakeHazard{ampacity: 900},
		issued, valid,
	)
	if err != nil {
		t.Fatalf("rate: %v", err)
	}

	if unjoined.Located != 0 || unjoined.WeatherRated != 0 {
		t.Errorf("no branch reaches a route, so nothing is located: %+v", unjoined)
	}

	// The same five branches, every one of them with a route and spans, and
	// no ampacity computed for any of those spans.
	unrated, err := rate(
		t.Context(), fiveBranchModel(345_000),
		&fakeGeo{route: aRoute, spans: threeSpans},
		&fakeHazard{bySpan: map[id.ID]float64{}},
		issued, valid,
	)
	if err != nil {
		t.Fatalf("rate: %v", err)
	}

	if unrated.Located != 5 {
		t.Errorf("Located = %d, want 5 — every branch reached its spans and only "+
			"the weather is missing", unrated.Located)
	}

	if unrated.WeatherRated != 0 {
		t.Errorf("WeatherRated = %d, want 0", unrated.WeatherRated)
	}
}

// A located branch counts as located whether or not it was rated, so the two
// numbers together say how far down the chain a run got.
func TestLocatedCountsRatedBranchesToo(t *testing.T) {
	t.Parallel()

	got, err := rate(
		t.Context(), fiveBranchModel(345_000),
		&fakeGeo{route: aRoute, spans: threeSpans}, &fakeHazard{ampacity: 900},
		issued, valid,
	)
	if err != nil {
		t.Fatalf("rate: %v", err)
	}

	if got.Located != 5 || got.WeatherRated != 5 {
		t.Errorf("located %d and rated %d of 5", got.Located, got.WeatherRated)
	}
}

// A network at two voltage levels, which is what a transmission model
// normally is. Branch 0 runs from a 345 kV bus, branch 1 from a 138 kV bus,
// and the model base is 345 kV because that is the level the importer's
// per-unit base happened to come from.
func twoVoltageModel() *solverv1.NetworkModel {
	volts := func(v float64) *float64 { return &v }

	return &solverv1.NetworkModel{
		Name:    "two voltages",
		SBaseVa: 100e6,
		VBaseV:  345e3,
		Buses: []*solverv1.Bus{
			{Id: "bus-1", VNominalV: volts(345e3)},
			{Id: "bus-2", VNominalV: volts(138e3)},
			// States none, so a branch leaving it falls back to the base.
			{Id: "bus-3"},
		},
		Branches: []*solverv1.Branch{
			{Id: lines[0].String(), FromBus: 0, ToBus: 1, RatingVa: 500e6, InService: true, Tap: 1},
			{Id: lines[1].String(), FromBus: 1, ToBus: 2, RatingVa: 500e6, InService: true, Tap: 1},
			{Id: lines[2].String(), FromBus: 2, ToBus: 0, RatingVa: 500e6, InService: true, Tap: 1},
		},
	}
}

// The defect this whole change exists for, and the one test that must fail
// against the single-base code.
//
// Every branch used to convert against the model's one voltage base. On a
// mixed-voltage network that is right for the branches that happen to sit at
// the base's level and wrong for the rest: the 138 kV branch below came out
// at √3 × 345 kV × 900 A, which is 2.5 times its real rating — correct
// arithmetic on the wrong input, carried into the contingency screen and the
// cascade, where 2.5 times a line's capacity is the difference between a
// reported overload and a silent one.
func TestEachBranchConvertsAgainstItsOwnFromBusVoltage(t *testing.T) {
	t.Parallel()

	got, err := rate(
		t.Context(), twoVoltageModel(),
		&fakeGeo{route: aRoute, spans: threeSpans},
		&fakeHazard{ampacity: 900},
		issued, valid,
	)
	if err != nil {
		t.Fatalf("rate: %v", err)
	}

	if len(got.Overrides) != 3 {
		t.Fatalf("got %d overrides, want one per branch", len(got.Overrides))
	}

	// Branch 0 leaves the 345 kV bus, branch 1 leaves the 138 kV bus, and
	// branch 2 leaves the bus that states nothing and so falls back to the
	// 345 kV base.
	want := []float64{
		math.Sqrt(3) * 345e3 * 900,
		math.Sqrt(3) * 138e3 * 900,
		math.Sqrt(3) * 345e3 * 900,
	}

	for index, expected := range want {
		rating := got.Overrides[index].GetBranchRating()

		if rating.GetBranch() != uint32(index) {
			t.Fatalf("override %d names branch %d", index, rating.GetBranch())
		}

		if math.Abs(rating.GetRatingVa()-expected) > 1 {
			t.Errorf("branch %d is rated %v VA, want %v — a factor of %.2f",
				index, rating.GetRatingVa(), expected,
				rating.GetRatingVa()/expected)
		}
	}
}

// twoVoltageModel's third branch used to be this suite's one fallback: its
// from-bus, bus-3, states nothing, and under the old from-bus-only rule that
// was enough to send it to the base. It no longer is. Under stated-end-first
// the same branch's to-bus is bus-1, which states 345 kV, and a real voltage
// at the branch's own far end beats the network base — so nothing in this
// model falls back at all. Genuine fallback, where neither end states a
// voltage, is TestNeitherEndStatedStillFallsBackToTheBase's job instead.
func TestTheFallbackToTheModelBaseIsCounted(t *testing.T) {
	t.Parallel()

	got, err := rate(
		t.Context(), twoVoltageModel(),
		&fakeGeo{route: aRoute, spans: threeSpans},
		&fakeHazard{ampacity: 900},
		issued, valid,
	)
	if err != nil {
		t.Fatalf("rate: %v", err)
	}

	if got.BaseVoltageFallback != 0 {
		t.Errorf("BaseVoltageFallback = %d, want 0 of %d rated branches — every "+
			"branch here has at least one end stating a voltage",
			got.BaseVoltageFallback, got.WeatherRated)
	}
}

// A model whose buses all state their own voltage substitutes nothing, and
// the count says so. Without this, a fallback counter stuck at the number of
// overrides would look correct in the test above.
func TestAModelThatStatesEveryVoltageFallsBackNowhere(t *testing.T) {
	t.Parallel()

	model := twoVoltageModel()
	stated := 138e3
	model.Buses[2].VNominalV = &stated

	got, err := rate(
		t.Context(), model,
		&fakeGeo{route: aRoute, spans: threeSpans},
		&fakeHazard{ampacity: 900},
		issued, valid,
	)
	if err != nil {
		t.Fatalf("rate: %v", err)
	}

	if got.BaseVoltageFallback != 0 {
		t.Errorf("BaseVoltageFallback = %d, want none — every bus states a voltage",
			got.BaseVoltageFallback)
	}

	// And the third branch now converts at 138 kV rather than at the base.
	want := math.Sqrt(3) * 138e3 * 900
	if rating := got.Overrides[2].GetBranchRating().GetRatingVa(); math.Abs(rating-want) > 1 {
		t.Errorf("branch 2 is rated %v VA, want %v", rating, want)
	}
}

// A bus stating zero volts is a bus stating nothing, not a bus at no voltage.
// Converting against it would emit a rating of zero, which every consumer
// reads as unrated and skips — the branch would leave every violation check
// rather than fail one.
func TestAZeroNominalVoltageIsTreatedAsAbsent(t *testing.T) {
	t.Parallel()

	model := twoVoltageModel()
	zero := 0.0
	model.Buses[1].VNominalV = &zero

	got, err := rate(
		t.Context(), model,
		&fakeGeo{route: aRoute, spans: threeSpans},
		&fakeHazard{ampacity: 900},
		issued, valid,
	)
	if err != nil {
		t.Fatalf("rate: %v", err)
	}

	// Branch 1 leaves bus-2, which now says zero.
	rating := got.Overrides[1].GetBranchRating().GetRatingVa()

	if rating <= 0 {
		t.Fatalf("branch 1 is rated %v VA, which every consumer reads as unrated", rating)
	}

	if want := math.Sqrt(3) * 345e3 * 900; math.Abs(rating-want) > 1 {
		t.Errorf("branch 1 is rated %v VA, want the base's %v", rating, want)
	}
}

// A branch whose from-bus states nothing but whose to-bus does, in a model
// whose base is a different voltage again. This is the exact shape §5.1 of
// the branch-side conductor design names: a from-bus-only rule falls back to
// the 345 kV base and rates a 138 kV line 2.5 times over its own capacity —
// correct arithmetic on the wrong voltage, carried into the contingency
// screen and the cascade. hazard's own statedEndFirst already reads this
// branch as 138 kV; branchVolts has to agree, or the same network answers
// "what voltage does this branch run at" two different ways depending on
// which command asked.
func fromSilentToStatedModel() *solverv1.NetworkModel {
	volts := func(v float64) *float64 { return &v }

	return &solverv1.NetworkModel{
		Name:    "from silent, to stated",
		SBaseVa: 100e6,
		VBaseV:  345e3,
		Buses: []*solverv1.Bus{
			{Id: "bus-1"},                          // states nothing
			{Id: "bus-2", VNominalV: volts(138e3)}, // the far end
		},
		Branches: []*solverv1.Branch{
			{Id: lines[0].String(), FromBus: 0, ToBus: 1, RatingVa: 500e6, InService: true, Tap: 1},
		},
	}
}

func TestABranchWhoseFromBusIsSilentConvertsAtItsToBusVoltage(t *testing.T) {
	t.Parallel()

	got, err := rate(
		t.Context(), fromSilentToStatedModel(),
		&fakeGeo{route: aRoute, spans: threeSpans},
		&fakeHazard{ampacity: 900},
		issued, valid,
	)
	if err != nil {
		t.Fatalf("rate: %v", err)
	}

	if len(got.Overrides) != 1 {
		t.Fatalf("got %d overrides, want 1", len(got.Overrides))
	}

	want := math.Sqrt(3) * 138e3 * 900
	if rating := got.Overrides[0].GetBranchRating().GetRatingVa(); math.Abs(rating-want) > 1 {
		t.Errorf("rating = %v VA, want %v (138 kV, the to-bus) — %v would be the "+
			"345 kV base, 2.5 times over", rating, want, math.Sqrt(3)*345e3*900)
	}

	// The to-bus's own voltage is the branch's own voltage, not a substitute
	// for one: this must not be counted alongside a real fallback.
	if got.BaseVoltageFallback != 0 {
		t.Errorf("BaseVoltageFallback = %d, want 0 — the to-bus stated a real voltage",
			got.BaseVoltageFallback)
	}
}

// The from end is still preferred: a branch whose two ends state different
// voltages — the transformer shape — still converts at the from end,
// unchanged from before this rule gained a to-bus case at all.
func TestATransformerShapedBranchStillUsesTheFromEnd(t *testing.T) {
	t.Parallel()

	volts := func(v float64) *float64 { return &v }

	model := &solverv1.NetworkModel{
		Name:    "transformer shape",
		SBaseVa: 100e6,
		VBaseV:  345e3,
		Buses: []*solverv1.Bus{
			{Id: "bus-1", VNominalV: volts(345e3)},
			{Id: "bus-2", VNominalV: volts(138e3)},
		},
		Branches: []*solverv1.Branch{
			{Id: lines[0].String(), FromBus: 0, ToBus: 1, RatingVa: 500e6, InService: true, Tap: 1},
		},
	}

	got, err := rate(
		t.Context(), model,
		&fakeGeo{route: aRoute, spans: threeSpans},
		&fakeHazard{ampacity: 900},
		issued, valid,
	)
	if err != nil {
		t.Fatalf("rate: %v", err)
	}

	want := math.Sqrt(3) * 345e3 * 900
	if rating := got.Overrides[0].GetBranchRating().GetRatingVa(); math.Abs(rating-want) > 1 {
		t.Errorf("rating = %v VA, want %v — the from end, even though the to end "+
			"also states one", rating, want)
	}

	if got.BaseVoltageFallback != 0 {
		t.Errorf("BaseVoltageFallback = %d, want 0 — the from-bus stated its own voltage",
			got.BaseVoltageFallback)
	}
}

// Neither end stating a voltage is still the one case the model base exists
// for, and it is still counted.
func TestNeitherEndStatedStillFallsBackToTheBase(t *testing.T) {
	t.Parallel()

	model := &solverv1.NetworkModel{
		Name:    "neither end stated",
		SBaseVa: 100e6,
		VBaseV:  345e3,
		Buses: []*solverv1.Bus{
			{Id: "bus-1"},
			{Id: "bus-2"},
		},
		Branches: []*solverv1.Branch{
			{Id: lines[0].String(), FromBus: 0, ToBus: 1, RatingVa: 500e6, InService: true, Tap: 1},
		},
	}

	got, err := rate(
		t.Context(), model,
		&fakeGeo{route: aRoute, spans: threeSpans},
		&fakeHazard{ampacity: 900},
		issued, valid,
	)
	if err != nil {
		t.Fatalf("rate: %v", err)
	}

	want := math.Sqrt(3) * 345e3 * 900
	if rating := got.Overrides[0].GetBranchRating().GetRatingVa(); math.Abs(rating-want) > 1 {
		t.Errorf("rating = %v VA, want %v — the model base, neither end states one",
			rating, want)
	}

	if got.BaseVoltageFallback != 1 {
		t.Errorf("BaseVoltageFallback = %d, want 1", got.BaseVoltageFallback)
	}
}

// A to-bus index outside the model's bus list is a broken model, exactly
// like an out-of-range from-bus index, and this function's job is still to
// pick a voltage rather than invent an error the rating step cannot fix.
func TestAToBusIndexOutOfRangeFallsBackRatherThanPanicking(t *testing.T) {
	t.Parallel()

	model := &solverv1.NetworkModel{
		Name:    "broken to-bus index",
		SBaseVa: 100e6,
		VBaseV:  345e3,
		Buses: []*solverv1.Bus{
			{Id: "bus-1"}, // states nothing, so branchVolts must look at ToBus
		},
		Branches: []*solverv1.Branch{
			{Id: lines[0].String(), FromBus: 0, ToBus: 7, RatingVa: 500e6, InService: true, Tap: 1},
		},
	}

	got, err := rate(
		t.Context(), model,
		&fakeGeo{route: aRoute, spans: threeSpans},
		&fakeHazard{ampacity: 900},
		issued, valid,
	)
	if err != nil {
		t.Fatalf("rate: %v", err)
	}

	want := math.Sqrt(3) * 345e3 * 900
	if rating := got.Overrides[0].GetBranchRating().GetRatingVa(); math.Abs(rating-want) > 1 {
		t.Errorf("rating = %v VA, want %v — an out-of-range to-bus falls back to the base",
			rating, want)
	}

	if got.BaseVoltageFallback != 1 {
		t.Errorf("BaseVoltageFallback = %d, want 1", got.BaseVoltageFallback)
	}
}
