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
	"math"
	"time"

	"github.com/xraph/blackstart/extensions/geo/asset"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/quantity"
)

// GeoReader is the geometry this needs and nothing more.
//
// Narrow because the traversal below is the only geometry a scenario has any
// business doing: a wider interface would let the rating step reach for a
// route's provenance or a span's azimuth and quietly acquire opinions about
// the asset record. *geo.Engine satisfies it as it stands.
type GeoReader interface {
	// RouteForBranch finds the route carrying an electrical branch.
	RouteForBranch(ctx context.Context, branch id.ID) (asset.Route, error)

	// Spans reads a route's spans in order.
	Spans(ctx context.Context, route id.ID) ([]asset.Span, error)
}

// HazardReader reads one computed ampacity, in AMPERES.
//
// Both time axes are parameters rather than defaults. hazard stores its values
// under a series key of (span, quantity, issuance) with the valid time as the
// position inside that series, so naming only one axis selects an arbitrary
// member of a series rather than a specific number — a rating quietly taken
// from the wrong forecast hour is exactly the failure this pipeline must not
// have.
type HazardReader interface {
	// Ampacity is the thermal rating computed for this span, from the weather
	// issued at issuedAt, describing the moment validAt.
	//
	// A span with no computed value returns an error rather than a zero: the
	// caller has to be able to tell "unknown" from "cannot carry anything".
	Ampacity(ctx context.Context, span id.ID, issuedAt, validAt time.Time) (float64, error)
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
// Reports false for any break in the chain: an identifier the platform cannot
// parse, no route, no spans, or any span without a usable computed value. A
// partial answer is refused rather than returned, because a minimum taken over
// the spans that happened to resolve is a rating for a shorter line than the
// one being rated.
func weakestSpan(
	ctx context.Context,
	geo GeoReader,
	hazard HazardReader,
	branchID string,
	issuedAt, validAt time.Time,
) (float64, bool) {
	branch, err := id.Parse(branchID)
	if err != nil {
		return 0, false
	}

	route, err := geo.RouteForBranch(ctx, branch)
	if err != nil {
		return 0, false
	}

	spans, err := geo.Spans(ctx, route.ID)
	if err != nil || len(spans) == 0 {
		return 0, false
	}

	weakest := math.Inf(1)

	for _, span := range spans {
		amperes, ampErr := hazard.Ampacity(ctx, span.ID, issuedAt, validAt)
		if ampErr != nil {
			return 0, false
		}

		// A non-positive or non-finite ampacity is not a measurement of a line
		// that can carry nothing; it is a value that should not exist. Letting
		// one through would make it the minimum and hand the solver a branch
		// that is overloaded by construction.
		if !(amperes > 0) || math.IsInf(amperes, 0) {
			return 0, false
		}

		weakest = math.Min(weakest, amperes)
	}

	return weakest, true
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
