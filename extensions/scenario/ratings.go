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

// weakestSpan is the lowest ampacity across a branch's spans, in AMPERES.
//
// The minimum rather than a mean, and this is a physical claim rather than a
// choice of summary statistic: a transmission line is a series chain, so the
// one span crossing an exposed ridge in full sun limits the whole circuit
// however cool the rest of it is. A mean would report a line as able to carry
// current that one of its own spans cannot. Selection, not computation — the
// number returned is one hazard already computed, unaltered.
//
// Three outcomes, not two. A rating; a gap in the asset record, which is
// (0, false, nil) and leaves the branch on its static rating; or an error,
// which fails the whole run. A partial rating is never returned: a minimum
// taken over the spans that happened to resolve is a rating for a shorter
// line than the one being rated.
func weakestSpan(
	ctx context.Context,
	geo GeoReader,
	hazard HazardReader,
	branchID string,
	issuedAt, validAt time.Time,
) (float64, bool, error) {
	branch, named := parseBranch(branchID)
	if !named {
		return 0, false, nil
	}

	route, err := geo.RouteForBranch(ctx, branch)
	if err != nil {
		if isRatingGap(err) {
			return 0, false, nil
		}

		return 0, false, fmt.Errorf("reading the route carrying %s: %w", branch, err)
	}

	spans, err := geo.Spans(ctx, route.ID)
	if err != nil {
		if isRatingGap(err) {
			return 0, false, nil
		}

		return 0, false, fmt.Errorf("reading the spans of route %s: %w", route.ID, err)
	}

	if len(spans) == 0 {
		return 0, false, nil
	}

	ids := make([]id.ID, 0, len(spans))
	for _, span := range spans {
		ids = append(ids, span.ID)
	}

	amperes, err := hazard.Ampacities(ctx, ids, issuedAt, validAt)
	if err != nil {
		if isRatingGap(err) {
			return 0, false, nil
		}

		return 0, false, fmt.Errorf("reading the ampacities of route %s: %w", route.ID, err)
	}

	weakest := math.Inf(1)

	for _, span := range spans {
		value, ok := amperes[span.ID]
		if !ok {
			return 0, false, nil
		}

		// A non-positive or non-finite ampacity is not a measurement of a line
		// that can carry nothing; it is a value that should not exist. Letting
		// one through would make it the minimum and emit an override of zero,
		// which every consumer reads as unrated and skips — so the branch
		// would drop out of every violation check rather than fail one.
		if !(value > 0) || math.IsInf(value, 0) {
			return 0, false, nil
		}

		weakest = math.Min(weakest, value)
	}

	return weakest, true, nil
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
