package geo

import (
	"net/http"

	"github.com/xraph/forge"

	"github.com/xraph/blackstart/pkg/apiauth"

	"github.com/xraph/blackstart/extensions/geo/asset"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/pkg/httperr"
	"github.com/xraph/blackstart/pkg/id"
)

// --- Request DTOs ---

type listRoutesRequest struct {
	Limit int `query:"limit" description:"Maximum results" optional:"true"`
}

type routeRequest struct {
	ID string `path:"id" description:"Route identifier (rte_…)"`
}

type branchRouteRequest struct {
	ID string `path:"id" description:"Branch identifier"`
}

// --- Response DTOs ---

type routesResponse struct {
	Routes []asset.Route `json:"routes"`
}

// spansResponse is a route's geometry, span by span.
//
// The spans carry their own length and bearing rather than leaving a caller to
// derive them from the coordinates. A crosswind on a span is the component
// perpendicular to it, and a client recomputing that from two points would be
// a second implementation of the geodesy — which is the one thing this
// platform keeps in exactly one place.
type spansResponse struct {
	RouteID string       `json:"route_id"`
	Spans   []asset.Span `json:"spans"`

	// LengthM is the route's stored length, so a caller can check it against
	// the spans it just received rather than trusting either alone.
	LengthM float64 `json:"length_m"`
}

// --- Controller ---

// Controller exposes where the network physically is.
//
// Read-only. Geometry arrives by tracing a branch through the import pipeline,
// where its provenance — surveyed or interpolated — is recorded. An endpoint
// accepting geometry inline would produce spans that nothing could say the
// origin of, and a per-span failure probability conditioned on a path of
// unknown provenance is not a claim anyone should act on.
type Controller struct {
	engine *Engine
	guard  *apiauth.Guard
}

// NewController builds the HTTP surface over an engine.
func NewController(engine *Engine, guard *apiauth.Guard) *Controller {
	return &Controller{engine: engine, guard: guard}
}

// Name implements forge.Controller.
func (c *Controller) Name() string { return "geo:controller" }

// Prefix returns the base path for geo routes.
func (c *Controller) Prefix() string { return "/api/v1/geo" }

// Middlewares returns per-controller middleware.
func (c *Controller) Middlewares() []forge.Middleware { return c.guard.Middlewares() }

// Routes registers the read endpoints.
func (c *Controller) Routes(router forge.Router) error {
	var firstErr error

	check := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	check(router.GET("/routes", c.ListRoutes,
		forge.WithName("geo.routes.list"),
		forge.WithSummary("List traced routes"),
		forge.WithTags("Geo"),
		forge.WithRequestSchema(listRoutesRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Routes", routesResponse{}),
	))

	check(router.GET("/routes/:id", c.GetRoute,
		forge.WithName("geo.routes.get"),
		forge.WithSummary("Get a route"),
		forge.WithDescription(
			"A branch's path over the ground, with the provenance of that "+
				"path: surveyed means a source recorded where the structures "+
				"actually are, interpolated means a straight great-circle line "+
				"with evenly spaced imaginary ones. A per-span failure "+
				"probability conditioned on the second is a different claim "+
				"from one conditioned on the first."),
		forge.WithTags("Geo"),
		forge.WithRequestSchema(routeRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Route", asset.Route{}),
		forge.WithResponseSchema(http.StatusNotFound, "No such route", nil),
	))

	check(router.GET("/routes/:id/spans", c.GetSpans,
		forge.WithName("geo.routes.spans"),
		forge.WithSummary("Get a route's spans in order"),
		forge.WithDescription(
			"Geometry is per span because failure is per span: a conductor "+
				"breaks between two structures, an ice load is local, and a "+
				"200 km line passes through several weather systems at once."),
		forge.WithTags("Geo"),
		forge.WithRequestSchema(routeRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Spans", spansResponse{}),
		forge.WithResponseSchema(http.StatusNotFound, "No such route", nil),
	))

	check(router.GET("/branches/:id/route", c.GetBranchRoute,
		forge.WithName("geo.branches.route"),
		forge.WithSummary("Find the route carrying a branch"),
		forge.WithTags("Geo"),
		forge.WithRequestSchema(branchRouteRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Route", asset.Route{}),
		forge.WithResponseSchema(http.StatusNotFound, "This branch has no route", nil),
	))

	return firstErr
}

// ListRoutes returns traced routes.
func (c *Controller) ListRoutes(ctx forge.Context) error {
	var req listRoutesRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	found, err := c.engine.Routes(ctx.Request().Context(), req.Limit)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, routesResponse{Routes: found})
}

// GetRoute returns one route.
func (c *Controller) GetRoute(ctx forge.Context) error {
	want, err := c.route(ctx)
	if err != nil {
		return err
	}

	found, err := c.engine.Route(ctx.Request().Context(), want)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, found)
}

// GetSpans returns a route's spans.
func (c *Controller) GetSpans(ctx forge.Context) error {
	want, err := c.route(ctx)
	if err != nil {
		return err
	}

	inner := ctx.Request().Context()

	route, err := c.engine.Route(inner, want)
	if err != nil {
		return httperr.From(err)
	}

	spans, err := c.engine.Spans(inner, want)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, spansResponse{
		RouteID: route.ID.String(),
		Spans:   spans,
		LengthM: route.LengthM,
	})
}

// GetBranchRoute finds the route carrying an electrical branch.
func (c *Controller) GetBranchRoute(ctx forge.Context) error {
	var req branchRouteRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	branch, err := id.Parse(req.ID)
	if err != nil {
		return forge.BadRequest(err.Error())
	}

	found, err := c.engine.RouteForBranch(ctx.Request().Context(), branch)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, found)
}

// route reads and parses the route identifier from a request.
func (c *Controller) route(ctx forge.Context) (id.ID, error) {
	var req routeRequest
	if err := ctx.BindRequest(&req); err != nil {
		return id.Nil, forge.BadRequest(err.Error())
	}

	want, err := gid.ParseRoute(req.ID)
	if err != nil {
		return id.Nil, forge.BadRequest(err.Error())
	}

	return want, nil
}
