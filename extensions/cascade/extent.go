package cascade

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/blackstart/extensions/cascade/run"
	"github.com/xraph/blackstart/extensions/geo"
	"github.com/xraph/blackstart/extensions/geo/asset"
	"github.com/xraph/blackstart/extensions/geo/shape"
	nmmodel "github.com/xraph/blackstart/extensions/networkmodel/model"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Models resolves the network a cascade run simulated.
//
// One method, the one Extent needs: turning the model hash a run recorded
// back into the row naming its identifier. Satisfied by nmmodel.Store.
type Models interface {
	// FindByHash finds a stored model by its content hash.
	FindByHash(ctx context.Context, hash string) (nmmodel.NetworkModel, error)
}

// Branches resolves the ordinal a cascade event names to the branch it
// identifies.
//
// The whole reason branch identity exists: an event's Element is
// "branch-3", the solver's own name for a position in the file it read, not
// something a route can be looked up by. ByOrdinal is that lookup, and
// nmmodel.BranchStore's own doc names this extension as its caller.
type Branches interface {
	// ByOrdinal finds one branch by the name the solver uses for it. Returns
	// an error wrapping bserr.ErrNotFound when the model has no such ordinal.
	ByOrdinal(ctx context.Context, modelID id.ID, ordinal string) (nmmodel.Branch, error)
}

// Geo is the geometry Extent needs and nothing more.
//
// Narrow on purpose, the same reason Simulator is: a wider interface would
// let this reach for a route's provenance or a structure's position, and
// every method here is one a test has to fake.
type Geo interface {
	// RouteForBranch finds the route carrying an electrical branch. Returns
	// an error wrapping bserr.ErrNotFound when the branch has no traced
	// route.
	RouteForBranch(ctx context.Context, branch id.ID) (asset.Route, error)

	// Spans reads a route's spans in order.
	Spans(ctx context.Context, route id.ID) ([]asset.Span, error)
}

// The real stores and engine satisfy these, checked here rather than
// asserted in prose: a comment claiming the shapes match is enforced by
// nothing, and the two drift the first time one of those packages changes a
// signature.
var (
	_ Models   = (nmmodel.Store)(nil)
	_ Branches = (nmmodel.BranchStore)(nil)
	_ Geo      = (*geo.Engine)(nil)
)

// ExtentResult is the ground a cascade run covered.
type ExtentResult struct {
	// Area is the ground within HalfWidthM of every traced route a tripped
	// branch carries. Corridors that overlap are returned whole rather than
	// merged — see shape.Cover — so this is not a single count of distinct
	// square meters, only every polygon known to have been affected.
	Area shape.MultiPolygon

	// HalfWidthM is the buffer used to build Area. Carried alongside it
	// because the area is uninterpretable without knowing how wide a
	// corridor around each conductor it assumes.
	HalfWidthM float64

	// Resolved is how many tripped branches' ordinals were found in the
	// model, whether or not their route could be traced.
	Resolved int

	// Unresolved is every tripped branch's ordinal the model has no branch
	// for. Reported rather than dropped: it means the run and the model
	// disagree, which is worth knowing regardless of what it does to the
	// area computed.
	Unresolved []string

	// Untraced is how many resolved, tripped branches had no traced route —
	// no route at all, or a route with no spans. Counted separately from
	// Unresolved because the disagreement is different: the branch is real
	// and known to the model, only its geometry is missing. A network only
	// partly traced must not read as a small blast radius, so this number
	// exists to say how much of the trip count Area could not draw from.
	Untraced int
}

// Extent is the ground a cascade run's tripped branches covered.
//
// Only run.EventTrip events contribute: an overload that never became a trip
// took no ground. Each trip's element is the solver's own ordinal for the
// branch, resolved against the model the run simulated — never the run's
// Initiating list, which names what started the cascade rather than what it
// went on to cover.
//
// # Errors
//
// Returns an error when the run cannot be read, its model cannot be
// resolved, or a collaborator fails for a reason other than the branch or
// its route being absent. An ordinal the model does not have, or a branch
// with no traced route, is recorded on the result instead of failing the
// call.
func (e *Engine) Extent(ctx context.Context, runID id.ID, halfWidthM float64) (ExtentResult, error) {
	if e.models == nil || e.branches == nil || e.geo == nil {
		return ExtentResult{}, errors.New(
			"cascade: this engine has no model, branch or geometry reader; " +
				"construct it with WithExtent to compute an extent")
	}

	if halfWidthM <= 0 {
		return ExtentResult{}, fmt.Errorf(
			"%w: an extent needs a positive half-width", bserr.ErrInvalid)
	}

	found, err := e.store.Runs().Get(ctx, runID)
	if err != nil {
		return ExtentResult{}, fmt.Errorf("cascade: reading run %s: %w", runID, err)
	}

	model, err := e.models.FindByHash(ctx, found.ModelHash)
	if err != nil {
		return ExtentResult{}, fmt.Errorf(
			"cascade: resolving the model run %s simulated: %w", runID, err)
	}

	result := ExtentResult{HalfWidthM: halfWidthM}

	var corridors []shape.Polygon

	for _, event := range found.Events {
		if event.Kind != run.EventTrip {
			continue
		}

		branch, err := e.branches.ByOrdinal(ctx, model.ID, event.Element)
		if err != nil {
			if errors.Is(err, bserr.ErrNotFound) {
				result.Unresolved = append(result.Unresolved, event.Element)
				continue
			}

			return ExtentResult{}, fmt.Errorf(
				"cascade: resolving branch %s: %w", event.Element, err)
		}

		result.Resolved++

		ground, traced, err := e.branchGround(ctx, branch.ID, halfWidthM)
		if err != nil {
			return ExtentResult{}, err
		}

		if !traced {
			result.Untraced++
			continue
		}

		corridors = append(corridors, ground...)
	}

	result.Area = shape.Cover(corridors)

	return result, nil
}

// branchGround is the corridor around every span of one branch's route.
//
// traced is false, with no error, when the branch has no route or the route
// has no spans — the "counted, not dropped" case Extent's caller must not
// mistake for an infrastructure failure.
func (e *Engine) branchGround(
	ctx context.Context, branch id.ID, halfWidthM float64,
) (ground []shape.Polygon, traced bool, err error) {
	route, err := e.geo.RouteForBranch(ctx, branch)
	if err != nil {
		if errors.Is(err, bserr.ErrNotFound) {
			return nil, false, nil
		}

		return nil, false, fmt.Errorf(
			"cascade: reading the route for branch %s: %w", branch, err)
	}

	spans, err := e.geo.Spans(ctx, route.ID)
	if err != nil {
		if errors.Is(err, bserr.ErrNotFound) {
			return nil, false, nil
		}

		return nil, false, fmt.Errorf(
			"cascade: reading the spans of route %s: %w", route.ID, err)
	}

	if len(spans) == 0 {
		return nil, false, nil
	}

	polygons := make([]shape.Polygon, 0, len(spans))

	for _, span := range spans {
		corridor, err := shape.Corridor(span.Geometry, halfWidthM)
		if err != nil {
			return nil, false, fmt.Errorf(
				"cascade: building the corridor for span %s: %w", span.ID, err)
		}

		polygons = append(polygons, corridor)
	}

	return polygons, true, nil
}
