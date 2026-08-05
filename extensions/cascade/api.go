package cascade

import (
	"net/http"

	"github.com/xraph/forge"

	"github.com/xraph/blackstart/pkg/apiauth"

	cid "github.com/xraph/blackstart/extensions/cascade/id"
	"github.com/xraph/blackstart/extensions/cascade/run"
	"github.com/xraph/blackstart/extensions/geo/shape"
	"github.com/xraph/blackstart/pkg/httperr"
)

// defaultExtentHalfWidthM is what a request that names no half_width_m gets,
// documented here once and echoed in the WithDescription text below so the
// generated client states it too — a caller reading only the response would
// otherwise have to guess whether a bare number was requested or assumed.
const defaultExtentHalfWidthM = 500

// --- Request DTOs ---

type getCascadeRunRequest struct {
	ID string `path:"id" description:"Run identifier (casc_…)"`
}

type listCascadeRunsRequest struct {
	Hash  string `query:"modelHash" description:"Content hash of the network model"`
	Limit int    `query:"limit"     description:"Maximum results, most recent first" optional:"true"`
}

// --- Response DTOs ---

type cascadeRunsResponse struct {
	Runs []run.CascadeRun `json:"runs"`
}

type islandsResponse struct {
	RunID   string       `json:"run_id"`
	Islands []run.Island `json:"islands"`
}

type getCascadeExtentRequest struct {
	ID string `path:"id" description:"Run identifier (casc_…)"`

	HalfWidthM float64 `query:"half_width_m" description:"Corridor half-width in meters on each side of a traced route; defaults to 500" optional:"true"`
}

// extentResponse is the ground a cascade run's tripped branches covered.
//
// Area is corridor area, not distinct ground: shape.Cover, which built it,
// deliberately does not merge corridors that overlap, so summing the
// polygons here would overstate what was actually affected. See
// cascade.ExtentResult.
type extentResponse struct {
	RunID      string             `json:"run_id"`
	Area       shape.MultiPolygon `json:"area"`
	HalfWidthM float64            `json:"half_width_m"`
	Resolved   int                `json:"resolved"`
	Untraced   int                `json:"untraced"`
	Unresolved []string           `json:"unresolved"`
}

// multiply converts per unit to SI. The wire's complement of engine.go's
// divide: a CascadeRun stores LoadLost, Island.Balance and Island.Shed per
// unit of its own SBaseVA (see that field's comment), and this is the one
// place — named, like divide is — that converts them back to watts before
// they reach an HTTP response. "No power-systems mathematics in Go" permits
// exactly this: a unit conversion, not a solve.
func multiply(value, base float64) float64 {
	return value * base
}

// wireIslands converts a run's islands to the watts the HTTP surface
// publishes, leaving the stored, per-unit slice the engine returned
// untouched — every other field crosses unchanged.
func wireIslands(islands []run.Island, sBaseVA float64) []run.Island {
	out := make([]run.Island, len(islands))

	for i, isl := range islands {
		out[i] = isl
		out[i].Balance = multiply(isl.Balance, sBaseVA)
		out[i].Shed = multiply(isl.Shed, sBaseVA)
	}

	return out
}

// wireRun converts one recorded run to the watts the HTTP surface publishes.
// A copy: what Get/ListByModel returned, and what a caller might still hold,
// stays the per-unit record the store actually persists.
func wireRun(in run.CascadeRun) run.CascadeRun {
	out := in
	out.LoadLost = multiply(in.LoadLost, in.SBaseVA)
	out.Islands = wireIslands(in.Islands, in.SBaseVA)

	return out
}

// --- Controller ---

// Controller exposes recorded cascade simulations over HTTP.
//
// Read-only, for the same reason the other solver-backed APIs are: a
// simulation needs a network model, and models arrive through the import
// pipeline where their provenance is recorded. An endpoint accepting a model
// inline would produce findings with no dataset version behind them.
type Controller struct {
	engine *Engine
	guard  *apiauth.Guard
}

// NewController builds the HTTP surface over an engine.
func NewController(engine *Engine, guard *apiauth.Guard) *Controller {
	return &Controller{engine: engine, guard: guard}
}

// Name implements forge.Controller.
func (c *Controller) Name() string { return "cascade:controller" }

// Prefix returns the base path for cascade routes.
func (c *Controller) Prefix() string { return "/api/v1/cascade" }

// Middleware returns per-controller middleware.
//
// Singular, and that is not a style choice. Forge's router type-asserts
// ControllerWithMiddleware, whose method is Middleware() — a controller
// declaring Middlewares() satisfies nothing, the assertion fails silently, and
// every route registers unguarded. The assertion below makes that a compile
// error rather than an open API.
func (c *Controller) Middleware() []forge.Middleware { return c.guard.Middlewares() }

// Guarded at compile time. Renaming this method or changing its signature
// stops the build instead of quietly unprotecting the controller.
var _ forge.ControllerWithMiddleware = (*Controller)(nil)

// Routes registers the read endpoints.
func (c *Controller) Routes(router forge.Router) error {
	var firstErr error

	check := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	check(router.GET("/runs", c.ListRuns,
		forge.WithName("cascade.runs.list"),
		forge.WithSummary("List cascades for a network"),
		forge.WithDescription(
			"Every recorded cascade simulation of one network model, most recent first."),
		forge.WithTags("Cascade"),
		forge.WithRequestSchema(listCascadeRunsRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Runs", cascadeRunsResponse{}),
		forge.WithResponseSchema(http.StatusBadRequest, "No model hash given", nil),
	))

	check(router.GET("/runs/:id", c.GetRun,
		forge.WithName("cascade.runs.get"),
		forge.WithSummary("Get a cascade run"),
		forge.WithDescription(
			"One recorded simulation, with every event in the order it happened "+
				"in simulated time."),
		forge.WithTags("Cascade"),
		forge.WithRequestSchema(getCascadeRunRequest{}),
		forge.WithResponseSchema(http.StatusOK, "CascadeRun", run.CascadeRun{}),
		forge.WithResponseSchema(http.StatusNotFound, "No such run", nil),
	))

	check(router.GET("/runs/:id/islands", c.GetIslands,
		forge.WithName("cascade.runs.islands"),
		forge.WithSummary("Get a cascade run's final islands"),
		forge.WithDescription(
			"The islanded state the network settled, truncated, or collapsed "+
				"into, with each island's worst condition over the run and its "+
				"final AC verdict."),
		forge.WithTags("Cascade"),
		forge.WithRequestSchema(getCascadeRunRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Islands", islandsResponse{}),
		forge.WithResponseSchema(http.StatusNotFound, "No such run", nil),
	))

	check(router.GET("/runs/:id/extent", c.GetExtent,
		forge.WithName("cascade.runs.extent"),
		forge.WithSummary("Get a cascade run's extent"),
		forge.WithDescription(
			"The corridor area within half_width_m (default 500, in meters on "+
				"each side of a traced route) of every branch the run tripped. "+
				"Corridors that overlap are returned whole rather than merged, "+
				"so this is corridor area, not a single figure of distinct "+
				"ground — see shape.Cover."),
		forge.WithTags("Cascade"),
		forge.WithRequestSchema(getCascadeExtentRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Extent", extentResponse{}),
		forge.WithResponseSchema(http.StatusBadRequest, "Invalid half_width_m", nil),
		forge.WithResponseSchema(http.StatusNotFound, "No such run", nil),
	))

	return firstErr
}

// ListRuns returns every recorded cascade of one network.
func (c *Controller) ListRuns(ctx forge.Context) error {
	var req listCascadeRunsRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	if req.Hash == "" {
		return forge.BadRequest(
			"modelHash is required; listing every run across every network is " +
				"not a question anyone asks and would page forever")
	}

	found, err := c.engine.ListByModel(ctx.Request().Context(), req.Hash, req.Limit)
	if err != nil {
		return httperr.From(err)
	}

	wire := make([]run.CascadeRun, len(found))
	for i, r := range found {
		wire[i] = wireRun(r)
	}

	return ctx.JSON(http.StatusOK, cascadeRunsResponse{Runs: wire})
}

// GetRun returns one recorded cascade, events in simulated-time order.
func (c *Controller) GetRun(ctx forge.Context) error {
	var req getCascadeRunRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	want, err := cid.ParseRun(req.ID)
	if err != nil {
		return forge.BadRequest(err.Error())
	}

	found, err := c.engine.Get(ctx.Request().Context(), want)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, wireRun(found))
}

// GetIslands returns a run's final islanded state.
func (c *Controller) GetIslands(ctx forge.Context) error {
	var req getCascadeRunRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	want, err := cid.ParseRun(req.ID)
	if err != nil {
		return forge.BadRequest(err.Error())
	}

	found, err := c.engine.Get(ctx.Request().Context(), want)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, islandsResponse{
		RunID:   found.ID.String(),
		Islands: wireIslands(found.Islands, found.SBaseVA),
	})
}

// GetExtent returns the ground a cascade run's tripped branches covered.
func (c *Controller) GetExtent(ctx forge.Context) error {
	var req getCascadeExtentRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	want, err := cid.ParseRun(req.ID)
	if err != nil {
		return forge.BadRequest(err.Error())
	}

	// A stated default rather than a hidden one: half_width_m left off the
	// query, or given as zero, both land here rather than reaching Extent as
	// a width of nothing.
	halfWidthM := req.HalfWidthM
	if halfWidthM <= 0 {
		halfWidthM = defaultExtentHalfWidthM
	}

	found, err := c.engine.Extent(ctx.Request().Context(), want, halfWidthM)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, extentResponse{
		RunID:      want.String(),
		Area:       found.Area,
		HalfWidthM: found.HalfWidthM,
		Resolved:   found.Resolved,
		Untraced:   found.Untraced,
		Unresolved: found.Unresolved,
	})
}
