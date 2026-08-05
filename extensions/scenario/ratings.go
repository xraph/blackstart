// Package scenario binds a network model, a weather cycle, hazard-derived line
// ratings, a contingency policy and a seed into one reproducible run.
//
// The domain types live in the scenario subpackage and the persistence in
// store; this package is the orchestration — the part that knows which engine
// to ask what, and in which order.
//
// Advisory only. Nothing here writes to a control system. See docs/SAFETY.md.
package scenario

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/xraph/blackstart/extensions/geo"
	"github.com/xraph/blackstart/extensions/geo/asset"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/quantity"
	solverv1 "github.com/xraph/blackstart/proto/gen/go/blackstart/solver/v1"
)

// GeoReader is the geometry this needs and nothing more.
//
// Narrow because the traversal below is the only geometry a scenario has any
// business doing: a wider interface would let the rating step reach for a
// route's provenance or a span's azimuth and quietly acquire opinions about
// the asset record.
type GeoReader interface {
	// RouteForBranch finds the route carrying an electrical branch.
	RouteForBranch(ctx context.Context, branch id.ID) (asset.Route, error)

	// Spans reads a route's spans in order.
	Spans(ctx context.Context, route id.ID) ([]asset.Span, error)
}

// The real engine satisfies it, checked here rather than asserted in prose:
// a comment claiming the shapes match is enforced by nothing, and the two
// drift the first time geo changes a signature.
var _ GeoReader = (*geo.Engine)(nil)

// HazardReader reads computed ampacities, in AMPERES.
//
// Both time axes are parameters rather than defaults. hazard stores its values
// under a series key of (span, quantity, issuance) with the valid time as the
// position inside that series, so naming only one axis selects an arbitrary
// member of a series rather than a specific number.
type HazardReader interface {
	// Ampacities reads the thermal rating computed for each of these spans,
	// from the weather issued at issuedAt, describing the moment validAt.
	//
	// A batch rather than a call per span, because hazard's own store is
	// shaped for it: ValueStore.Read takes a list of series keys so that "a
	// caller composes the keys it wants and asks once, rather than issuing one
	// round trip per series". A per-span reader would put two thousand
	// branches times twenty spans of sequential queries against a store built
	// to answer that in a few dozen — and a connection pool that saturates
	// partway through is precisely how a run comes back having rated half a
	// network without saying so.
	//
	// A span absent from the returned map has no computed value. An
	// implementation must read the EXACT instant validAt and let a miss be a
	// miss: never the nearest sample, never the enclosing hour. Widening the
	// window returns several members of one series and picking among them
	// silently is the wrong-forecast-hour failure both time axes exist to
	// prevent. If snapping is ever wanted it belongs in a named step with its
	// own test, not in the width of a window chosen inside an adapter.
	//
	// An error means the platform could not answer and fails the run. Absence
	// is the map; failure is the error. An implementation that collapses the
	// two turns a saturated pool into a network quietly left on nameplate.
	Ampacities(
		ctx context.Context, spans []id.ID, issuedAt, validAt time.Time,
	) (map[id.ID]float64, error)
}

// isRatingGap reports whether an error means the asset record is incomplete
// rather than that the platform failed to answer.
//
// The whole safety argument of this file rests on that distinction. A branch
// with no route is a known, permanent state and the run continues past it; a
// dropped connection or an expired deadline is neither, and reading one as
// the other is how two thousand branches come back with six hundred rated,
// no error, and a different answer every time the same pinned cycle is run.
//
// ErrInvalid counts as a gap because geo returns it for a carrier that cannot
// be solved — a circuit rather than a branch. That is a statement about the
// asset, not about the platform's health, and retrying will not change it.
func isRatingGap(err error) bool {
	return errors.Is(err, bserr.ErrNotFound) || errors.Is(err, bserr.ErrInvalid)
}

// parseBranch reads the wire's branch identifier as a platform one.
//
// The parse error is deliberately dropped rather than propagated.
// solverv1.Branch.id is a free string on the wire, so an importer that never
// minted platform identifiers produces a model this cannot join to the asset
// record at all — a permanent gap that would fail identically on every retry,
// which is the one thing it has in common with a branch that has no route and
// nothing in common with a dropped connection.
func parseBranch(branchID string) (id.ID, bool) {
	parsed, err := id.Parse(branchID)

	return parsed, err == nil
}

// spanOutcome is what the rating chain could say about one branch.
//
// Three states rather than two, because the caller has to tell the two
// unrated ones apart: a branch that never reached the asset record and a
// branch whose spans are known but carry no computed value are both "no
// override", and they are different failures with different fixes when a
// whole run comes back rated nothing.
type spanOutcome int

const (
	// outcomeUnlocated means the chain broke before any span was identified:
	// a branch identifier that is not a platform one, no route carrying the
	// branch, or a route with no spans.
	outcomeUnlocated spanOutcome = iota

	// outcomeUnrated means the spans are known and the weather has no usable
	// number for them at the pinned instant.
	outcomeUnrated

	// outcomeRated means every span answered and the branch has a rating.
	outcomeRated
)

// weakestSpan is the lowest ampacity across a branch's spans, in AMPERES.
//
// The minimum rather than a mean, and this is a physical claim rather than a
// choice of summary statistic: a transmission line is a series chain, so the
// one span crossing an exposed ridge in full sun limits the whole circuit
// however cool the rest of it is. A mean would report a line as able to carry
// current that one of its own spans cannot. Selection, not computation — the
// number returned is one hazard already computed, unaltered.
//
// Three outcomes and an error, not two. A rating; a gap that leaves the
// branch on its static rating, said two ways so the caller can tell a branch
// that never joined the asset record from one whose spans the weather did not
// reach; or an error, which fails the whole run. A partial rating is never
// returned: a minimum taken over the spans that happened to resolve is a
// rating for a shorter line than the one being rated.
func weakestSpan(
	ctx context.Context,
	geo GeoReader,
	hazard HazardReader,
	branchID string,
	issuedAt, validAt time.Time,
) (float64, spanOutcome, error) {
	branch, named := parseBranch(branchID)
	if !named {
		return 0, outcomeUnlocated, nil
	}

	route, err := geo.RouteForBranch(ctx, branch)
	if err != nil {
		if isRatingGap(err) {
			return 0, outcomeUnlocated, nil
		}

		return 0, outcomeUnlocated, fmt.Errorf("reading the route carrying %s: %w", branch, err)
	}

	spans, err := geo.Spans(ctx, route.ID)
	if err != nil {
		if isRatingGap(err) {
			return 0, outcomeUnlocated, nil
		}

		return 0, outcomeUnlocated, fmt.Errorf("reading the spans of route %s: %w", route.ID, err)
	}

	if len(spans) == 0 {
		return 0, outcomeUnlocated, nil
	}

	ids := make([]id.ID, 0, len(spans))
	for _, span := range spans {
		ids = append(ids, span.ID)
	}

	// Located from here on: the branch has spans, so anything that follows is
	// the weather missing rather than the asset record missing.
	amperes, err := hazard.Ampacities(ctx, ids, issuedAt, validAt)
	if err != nil {
		if isRatingGap(err) {
			return 0, outcomeUnrated, nil
		}

		return 0, outcomeUnrated, fmt.Errorf("reading the ampacities of route %s: %w", route.ID, err)
	}

	weakest := math.Inf(1)

	for _, span := range spans {
		value, ok := amperes[span.ID]
		if !ok {
			return 0, outcomeUnrated, nil
		}

		// A non-positive or non-finite ampacity is not a measurement of a line
		// that can carry nothing; it is a value that should not exist. Letting
		// one through would make it the minimum and emit an override of zero,
		// which every consumer reads as unrated and skips — so the branch
		// would drop out of every violation check rather than fail one.
		if !(value > 0) || math.IsInf(value, 0) {
			return 0, outcomeUnrated, nil
		}

		weakest = math.Min(weakest, value)
	}

	return weakest, outcomeRated, nil
}

// branchVolts is the line-to-line voltage a branch's ampacity converts
// against, in VOLTS, and whether the model-level base had to stand in.
//
// Selection, not computation, and the distinction is load-bearing here: no
// number is derived, averaged or scaled — one of two voltages the model
// already states is chosen and returned unaltered. The single arithmetic site
// in this package is voltAmperes, below, and it stays single.
//
// The from-bus's own nominal voltage, because the model-level base is one
// voltage for a network that may have several: a 138 kV line converted at a
// 345 kV base is rated 2.5 times too high, and the contingency screen and the
// cascade then run correct arithmetic on that. The base is the fallback rather
// than the default.
//
// The FROM end rather than the to end, an average, or the higher of the two.
// A transformer's ends are at different voltages and no single number is right
// for both, so the rule has to be stated rather than derived — and it has to
// be deterministic, because two runs of one pinned scenario must produce
// byte-identical requests. In practice every branch that reaches this point is
// a line whose ends are at one voltage: a transformer carries no route with
// spans, so weakestSpan returns outcomeUnlocated for it and no override is
// ever emitted. The rule matters for the case where that stops being true.
//
// A from-bus index outside the model's bus list falls back too, rather than
// failing. It is a broken model and the solver will say so about the model;
// this function's job is to pick a voltage, and inventing an error here would
// mean a rating step that fails for a reason the rating step cannot fix.
func branchVolts(
	model *solverv1.NetworkModel, branch *solverv1.Branch, base float64,
) (float64, bool) {
	buses := model.GetBuses()

	if from := int(branch.GetFromBus()); from >= 0 && from < len(buses) {
		// Absent is nil on the wire and reads as zero through the getter, and
		// a non-positive or infinite voltage is not a bus that sits at nothing
		// — it is a bus that states nothing. Converting against it would emit
		// a rating of zero, which every consumer reads as unrated and skips.
		if volts := buses[from].GetVNominalV(); volts > 0 && !math.IsInf(volts, 0) {
			return volts, false
		}
	}

	return base, true
}

// voltAmperes converts a line current into three-phase apparent power.
//
// Unit conversion, which is the only arithmetic this file is allowed: hazard
// computes ampacity in AMPERES and solverv1.Branch.rating_va is VOLT-AMPERES,
// and S = √3 · V · I is the definition of the latter for a balanced
// three-phase circuit at a line-to-line voltage V. Forgetting it leaves an
// ampere-scale number in a volt-ampere field — 800 where the answer is 4.78e8
// — which reads as a plausible small rating rather than as an error.
//
// The √3 comes from pkg/quantity rather than being written here, because
// bs-quantity states the same relationship on the Rust side as
// i_base = s_base / (√3 · v_base) and two spellings of a constant in one
// codebase is how they drift.
func voltAmperes(voltsLineToLine, amperes float64) float64 {
	return quantity.Sqrt3 * voltsLineToLine * amperes
}
