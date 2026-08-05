package cascade

import (
	"net/http"

	"github.com/xraph/forge"

	"github.com/xraph/blackstart/pkg/apiauth"

	cid "github.com/xraph/blackstart/extensions/cascade/id"
	"github.com/xraph/blackstart/extensions/cascade/run"
	"github.com/xraph/blackstart/pkg/httperr"
)

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
	Runs []run.Run `json:"runs"`
}

type islandsResponse struct {
	RunID   string       `json:"run_id"`
	Islands []run.Island `json:"islands"`
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
		forge.WithResponseSchema(http.StatusOK, "CascadeRun", run.Run{}),
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

	return ctx.JSON(http.StatusOK, cascadeRunsResponse{Runs: found})
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

	return ctx.JSON(http.StatusOK, found)
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
		Islands: found.Islands,
	})
}
