package scenario

// These tests live inside the package rather than in scenario_test because
// the rating step is unexported: it is one stage of a pipeline the engine
// owns, not a surface anything outside the extension calls.

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/xraph/blackstart/extensions/geo/asset"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	nmid "github.com/xraph/blackstart/extensions/networkmodel/id"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Package-level fixtures. A branch identifier carries the network-model prefix
// here because that is what geo means by a carrier that can be solved — see
// asset.CarriesElectrically.
var (
	errNotFound = bserr.ErrNotFound

	issued = time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	valid  = issued.Add(6 * time.Hour)

	aRoute = asset.Route{
		ID:        id.MustNew(gid.Route),
		CarrierID: id.MustNew(nmid.Model),
		LengthM:   3000,
	}

	threeSpans = []asset.Span{
		{ID: id.MustNew(gid.Span), RouteID: aRoute.ID, Sequence: 0, LengthM: 1000},
		{ID: id.MustNew(gid.Span), RouteID: aRoute.ID, Sequence: 1, LengthM: 1000},
		{ID: id.MustNew(gid.Span), RouteID: aRoute.ID, Sequence: 2, LengthM: 1000},
	}

	// Five branch identifiers, so a model builder and a fake can name the same
	// branch without either inventing an identifier the other cannot match.
	lines = [5]id.ID{
		id.MustNew(nmid.Model), id.MustNew(nmid.Model), id.MustNew(nmid.Model),
		id.MustNew(nmid.Model), id.MustNew(nmid.Model),
	}
)

// errBroken stands for everything that is not a documented absence: a reset
// connection, a saturated pool, an expired statement timeout. Deliberately
// wrapping neither bserr sentinel, because that is what makes it a failure.
var errBroken = errors.New("dial tcp 10.0.0.1:5432: connection reset by peer")

// fakeGeo is one route and one span list, or a route per branch when a test
// needs the chain complete for some branches and broken for others.
type fakeGeo struct {
	route          asset.Route
	routeErr       error
	routesByBranch map[string]asset.Route
	spans          []asset.Span
	spansErr       error

	// breakAfter makes RouteForBranch fail with errBroken once it has been
	// called this many times, which is how a connection pool gives out
	// partway through a model.
	breakAfter int
	calls      int
}

func (g *fakeGeo) RouteForBranch(_ context.Context, branch id.ID) (asset.Route, error) {
	g.calls++

	if g.breakAfter > 0 && g.calls > g.breakAfter {
		return asset.Route{}, errBroken
	}

	if g.routesByBranch != nil {
		route, ok := g.routesByBranch[branch.String()]
		if !ok {
			return asset.Route{}, errNotFound
		}

		return route, nil
	}

	if g.routeErr != nil {
		return asset.Route{}, g.routeErr
	}

	return g.route, nil
}

func (g *fakeGeo) Spans(_ context.Context, _ id.ID) ([]asset.Span, error) {
	if g.spansErr != nil {
		return nil, g.spansErr
	}

	return g.spans, nil
}

// fakeHazard answers with one ampacity for every span, or with a per-span
// number when a test cares which span is the weak one. A span it has no
// number for is simply absent from the map, which is the interface's way of
// saying no value was computed.
type fakeHazard struct {
	ampacity float64
	bySpan   map[id.ID]float64
	err      error

	// calls counts round trips, so a test can pin that one branch costs one
	// read rather than one per span.
	calls int
}

func (h *fakeHazard) Ampacities(
	_ context.Context, spans []id.ID, _, _ time.Time,
) (map[id.ID]float64, error) {
	h.calls++

	if h.err != nil {
		return nil, h.err
	}

	out := make(map[id.ID]float64, len(spans))

	for _, span := range spans {
		if h.bySpan == nil {
			out[span] = h.ampacity

			continue
		}

		if amperes, ok := h.bySpan[span]; ok {
			out[span] = amperes
		}
	}

	return out, nil
}

// spansCarrying builds a span per ampacity, in order, and the lookup a
// fakeHazard answers them from.
func spansCarrying(amperes []float64) ([]asset.Span, map[id.ID]float64) {
	spans := make([]asset.Span, 0, len(amperes))
	bySpan := make(map[id.ID]float64, len(amperes))

	for index, value := range amperes {
		span := asset.Span{ID: id.MustNew(gid.Span), RouteID: aRoute.ID, Sequence: index}
		spans = append(spans, span)
		bySpan[span.ID] = value
	}

	return spans, bySpan
}

// rateOneBranch rates a single branch whose spans carry the given ampacities,
// in order, and returns the volt-ampere rating that came out.
func rateOneBranch(t *testing.T, amperes []float64, volts float64) float64 {
	t.Helper()

	spans, bySpan := spansCarrying(amperes)

	weakest, ok, err := weakestSpan(
		t.Context(),
		&fakeGeo{route: aRoute, spans: spans},
		&fakeHazard{bySpan: bySpan},
		lines[0].String(), issued, valid,
	)
	if err != nil {
		t.Fatalf("weakestSpan: %v", err)
	}

	if !ok {
		t.Fatal("the chain is complete, so weakestSpan must report a rating")
	}

	return voltAmperes(volts, weakest)
}

// The rating is the weakest span, not the average.
//
// A line is a series chain: the span in full sun on an exposed ridge limits
// the whole circuit. Averaging would rate this line at 1100 A when one of its
// own spans can only carry 800.
func TestABranchIsRatedByItsWeakestSpan(t *testing.T) {
	t.Parallel()

	// Three spans at 1200 A, 800 A, 1300 A on a 345 kV branch.
	// Minimum 800 A → √3 × 345_000 × 800 = 4.78046e8 VA.
	got := rateOneBranch(t, []float64{1200, 800, 1300}, 345_000)

	want := math.Sqrt(3) * 345_000 * 800
	if math.Abs(got-want) > 1 {
		t.Errorf("rating = %v VA, want %v — averaging gives %v", got, want,
			math.Sqrt(3)*345_000*1100)
	}
}

// Amperes are not volt-amperes. A rating that forgot the conversion would be
// 800 where the answer is 4.78e8 — six orders of magnitude, and it would look
// like a plausible small number in a report.
func TestAmpacityConvertsToVoltAmperes(t *testing.T) {
	t.Parallel()

	got := rateOneBranch(t, []float64{1000}, 138_000)

	want := math.Sqrt(3) * 138_000 * 1000
	if math.Abs(got-want) > 1 {
		t.Errorf("rating = %v, want %v", got, want)
	}

	if got < 1e6 {
		t.Errorf("%v is an ampere-scale number — the conversion is missing", got)
	}
}

// A chain that breaks anywhere yields no rating at all, rather than a partial
// one. A minimum taken over the spans that happened to resolve is a rating for
// a shorter line than the one being rated, and a zero would make the branch
// infinitely overloaded.
func TestAChainThatBreaksYieldsNoRating(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		geo  *fakeGeo
		haz  *fakeHazard
	}{
		{
			name: "no route for the branch",
			geo:  &fakeGeo{routeErr: errNotFound},
			haz:  &fakeHazard{ampacity: 900},
		},
		{
			name: "route carries no spans",
			geo:  &fakeGeo{route: aRoute, spans: nil},
			haz:  &fakeHazard{ampacity: 900},
		},
		{
			name: "the ampacity read reports the span is unknown",
			geo:  &fakeGeo{route: aRoute, spans: threeSpans},
			haz:  &fakeHazard{err: errNotFound},
		},
		{
			// Absence in this interface is a span missing from the map, not
			// an error, and it has to read as the same gap.
			name: "spans have no computed ampacity",
			geo:  &fakeGeo{route: aRoute, spans: threeSpans},
			haz:  &fakeHazard{bySpan: map[id.ID]float64{}},
		},
		{
			// Not a gap in the record but a value that should not exist. It
			// has to be refused rather than taken as the minimum, or the
			// branch reaches the solver overloaded by construction.
			name: "a span reports a non-positive ampacity",
			geo:  &fakeGeo{route: aRoute, spans: threeSpans},
			haz:  &fakeHazard{ampacity: 0},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok, err := weakestSpan(
				t.Context(), tc.geo, tc.haz, lines[0].String(), issued, valid)
			if err != nil {
				t.Fatalf("a documented absence is a gap, not a run failure: %v", err)
			}

			if ok {
				t.Errorf("weakestSpan reported %v A from a broken chain", got)
			}
		})
	}
}

// An identifier the platform cannot parse is a broken chain like any other,
// and it is the one most likely to arrive: solverv1.Branch.id is a free string
// on the wire, so an importer that never minted platform identifiers produces
// a model full of them.
func TestABranchIdentifierThePlatformCannotParseIsARatingGap(t *testing.T) {
	t.Parallel()

	got, ok, err := weakestSpan(
		t.Context(),
		&fakeGeo{route: aRoute, spans: threeSpans},
		&fakeHazard{ampacity: 900},
		"ln1", issued, valid,
	)
	if err != nil {
		t.Fatalf("an identifier that will never parse is a gap, not a failure: %v", err)
	}

	if ok {
		t.Errorf("weakestSpan reported %v A for the unparsable branch %q", got, "ln1")
	}
}

// A dropped connection is not a line without geometry, and the difference has
// to survive every step of the chain.
//
// Read as a gap, a saturated pool would leave each remaining branch on its
// static rating, the run would return no error, and the same pinned cycle
// would produce a different override set on every attempt — the reproducibility
// invariant broken by an infrastructure hiccup, silently.
func TestAPlatformFailureIsNotAGapInTheAssetRecord(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		geo  *fakeGeo
		haz  *fakeHazard
	}{
		{
			name: "the route read fails",
			geo:  &fakeGeo{routeErr: errBroken},
			haz:  &fakeHazard{ampacity: 900},
		},
		{
			name: "the span read fails",
			geo:  &fakeGeo{route: aRoute, spansErr: errBroken},
			haz:  &fakeHazard{ampacity: 900},
		},
		{
			name: "the ampacity read fails",
			geo:  &fakeGeo{route: aRoute, spans: threeSpans},
			haz:  &fakeHazard{err: errBroken},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, ok, err := weakestSpan(
				t.Context(), tc.geo, tc.haz, lines[0].String(), issued, valid)
			if !errors.Is(err, errBroken) {
				t.Fatalf("weakestSpan returned (ok=%v, err=%v), want the transport error",
					ok, err)
			}
		})
	}
}

// One read per branch, not one per span. hazard's store takes a list of series
// keys precisely so a caller asks once; a per-span loop would put two thousand
// branches times twenty spans of sequential queries against it, which is both
// slow and the condition under which a pool times out midway.
func TestABranchCostsOneAmpacityRead(t *testing.T) {
	t.Parallel()

	haz := &fakeHazard{ampacity: 900}

	if _, _, err := weakestSpan(
		t.Context(),
		&fakeGeo{route: aRoute, spans: threeSpans}, haz,
		lines[0].String(), issued, valid,
	); err != nil {
		t.Fatalf("weakestSpan: %v", err)
	}

	if haz.calls != 1 {
		t.Errorf("%d ampacity reads for a %d-span branch, want 1",
			haz.calls, len(threeSpans))
	}
}
