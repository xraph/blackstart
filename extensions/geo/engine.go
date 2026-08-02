// Package geo owns where the network physically is.
//
// The one operation that matters in Phase 1 is Trace: two substation
// coordinates in, per-span geometry out. Everything else is storage around it.
package geo

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/blackstart/extensions/geo/asset"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/geo/shape"
	"github.com/xraph/blackstart/extensions/geo/store"
	"github.com/xraph/blackstart/pkg/hooks"
	"github.com/xraph/blackstart/pkg/id"
)

// DefaultSpanM is the nominal distance between structures.
//
// 400 m is a mid-range figure for high-voltage lattice towers on flat ground.
// Real spacing varies from under 200 m in rough terrain to over 500 m across
// open country, and the figure is configurable for that reason — but a default
// is needed because a route with no spacing has no spans, and one span per
// line is exactly the modelling error per-span geometry exists to avoid.
const DefaultSpanM = 400

// Engine holds the extension's business logic.
type Engine struct {
	store store.Store
	bus   *hooks.Bus
	spanM float64
}

// NewEngine builds the engine.
func NewEngine(s store.Store, spanM float64, bus *hooks.Bus) (*Engine, error) {
	if s == nil {
		return nil, errors.New("geo: engine needs a store")
	}

	if spanM <= 0 {
		spanM = DefaultSpanM
	}

	return &Engine{store: s, bus: bus, spanM: spanM}, nil
}

// TraceRequest asks for a route between two known points.
type TraceRequest struct {
	// BranchID names the electrical branch this route carries.
	BranchID id.ID

	// From and To are the branch's endpoints — usually its two substations.
	From, To shape.Point

	// SpanM overrides the nominal distance between structures. Zero uses the
	// engine's configured default.
	SpanM float64
}

// Trace builds and stores a synthetic route for a branch.
//
// The geometry is interpolated, and the stored route says so. That is not a
// disclaimer: a per-span failure probability conditioned on a straight-line
// guess is a different claim from one conditioned on a surveyed route, and
// recording which is which on the row means nothing downstream has to join
// through a provenance table to find out.
//
// # Errors
//
// Returns an error when either endpoint is off the earth or the route cannot
// be stored.
func (e *Engine) Trace(ctx context.Context, in TraceRequest) (asset.Route, error) {
	if in.BranchID.IsZero() {
		return asset.Route{}, errors.New(
			"geo: a route needs the branch it carries; a path over the ground " +
				"that carries nothing is not part of the network")
	}

	spanM := in.SpanM
	if spanM <= 0 {
		spanM = e.spanM
	}

	path, err := shape.SyntheticRoute(in.From, in.To, spanM)
	if err != nil {
		return asset.Route{}, fmt.Errorf("geo: tracing branch %s: %w", in.BranchID, err)
	}

	route := asset.Route{
		ID:         gid.NewRoute(),
		BranchID:   in.BranchID,
		Provenance: asset.Interpolated,
		LengthM:    path.LengthM(),
	}

	segments := path.Spans()
	spans := make([]asset.Span, 0, len(segments))

	for index, segment := range segments {
		spans = append(spans, asset.Span{
			ID:      gid.NewSpan(),
			RouteID: route.ID,
			// Numbered from one rather than zero, because a span number
			// appears in reports people read and "span 0" invites the question
			// of whether there is a span before it.
			Sequence:   index + 1,
			Geometry:   segment,
			LengthM:    segment.LengthM(),
			AzimuthDeg: segment[0].AzimuthTo(segment[len(segment)-1]),
		})
	}

	if err := e.store.Routes().Put(ctx, route, spans); err != nil {
		return asset.Route{}, fmt.Errorf("geo: storing the route for %s: %w", in.BranchID, err)
	}

	e.emit(ctx, route, len(spans))

	return route, nil
}

// Route reads one route.
func (e *Engine) Route(ctx context.Context, want id.ID) (asset.Route, error) {
	return e.store.Routes().Get(ctx, want)
}

// RouteForBranch finds the route carrying an electrical branch.
func (e *Engine) RouteForBranch(ctx context.Context, branch id.ID) (asset.Route, error) {
	return e.store.Routes().ForBranch(ctx, branch)
}

// Spans reads a route's spans in order.
func (e *Engine) Spans(ctx context.Context, route id.ID) ([]asset.Span, error) {
	return e.store.Routes().Spans(ctx, route)
}

// Routes lists stored routes.
func (e *Engine) Routes(ctx context.Context, limit int) ([]asset.Route, error) {
	return e.store.Routes().List(ctx, limit)
}

// emit publishes a domain event, best effort.
func (e *Engine) emit(ctx context.Context, route asset.Route, spans int) {
	if e.bus == nil {
		return
	}

	e.bus.Emit(ctx, hooks.Event{
		Name:      "geo.route.traced",
		Extension: "geo",
		Entity:    "Route",
		EntityID:  route.ID.String(),
		Op:        hooks.OpCreate,
		Payload:   map[string]any{"route": route, "spans": spans},
	})
}
