package contingency

import (
	"net/http"

	"github.com/xraph/forge"

	"github.com/xraph/blackstart/pkg/apiauth"

	cid "github.com/xraph/blackstart/extensions/contingency/id"
	"github.com/xraph/blackstart/extensions/contingency/sweep"
	"github.com/xraph/blackstart/pkg/httperr"
)

// --- Request DTOs ---

type getRunRequest struct {
	ID string `path:"id" description:"Run identifier (crun_…)"`
}

type listRunsRequest struct {
	Hash  string `query:"modelHash" description:"Content hash of the network model"`
	Limit int    `query:"limit"     description:"Maximum results, most recent first" optional:"true"`
}

type rankedRequest struct {
	ID string `path:"id" description:"Run identifier (crun_…)"`
	// Concerning-only by default, because a sweep over a large network
	// produces thousands of results and almost none of them are the answer.
	All bool `query:"all" description:"Return every contingency rather than only the concerning ones" optional:"true"`
}

// --- Response DTOs ---

type runsResponse struct {
	Runs []sweep.Run `json:"runs"`
}

// rankedResponse is the sweep's output in the order that matters.
//
// Unevaluated contingencies come first. That is not a presentation choice: a
// radial outage that islands part of the network is usually the most
// consequential thing on the list and is exactly what the linear screen cannot
// answer, so ranking it by a loading it does not have would bury it.
type rankedResponse struct {
	RunID      string            `json:"run_id"`
	Candidates []sweep.Candidate `json:"candidates"`

	// Unevaluated counts what the screen could not answer, so a caller sees
	// the number without having to filter the list to discover it.
	Unevaluated int `json:"unevaluated"`
}

// --- Controller ---

// Controller exposes recorded contingency analyses over HTTP.
//
// Read-only, for the same reason the power-flow API is: a sweep needs a
// network model, and models arrive through the import pipeline where their
// provenance is recorded. An endpoint accepting a model inline would produce
// findings with no dataset version behind them.
type Controller struct {
	engine *Engine
	guard  *apiauth.Guard
}

// NewController builds the HTTP surface over an engine.
func NewController(engine *Engine, guard *apiauth.Guard) *Controller {
	return &Controller{engine: engine, guard: guard}
}

// Name implements forge.Controller.
func (c *Controller) Name() string { return "contingency:controller" }

// Prefix returns the base path for contingency routes.
func (c *Controller) Prefix() string { return "/api/v1/contingency" }

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
		forge.WithName("contingency.runs.list"),
		forge.WithSummary("List sweeps for a network"),
		forge.WithDescription(
			"Every recorded N-1 analysis of one network model, most recent first."),
		forge.WithTags("Contingency"),
		forge.WithRequestSchema(listRunsRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Runs", runsResponse{}),
		forge.WithResponseSchema(http.StatusBadRequest, "No model hash given", nil),
	))

	check(router.GET("/runs/:id", c.GetRun,
		forge.WithName("contingency.runs.get"),
		forge.WithSummary("Get a sweep"),
		forge.WithDescription(
			"One recorded analysis, with every contingency in the order it was "+
				"evaluated. That order is preserved because it is what was asked "+
				"for; ranking is a separate view."),
		forge.WithTags("Contingency"),
		forge.WithRequestSchema(getRunRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Run", sweep.Run{}),
		forge.WithResponseSchema(http.StatusNotFound, "No such run", nil),
	))

	check(router.GET("/runs/:id/ranked", c.GetRanked,
		forge.WithName("contingency.runs.ranked"),
		forge.WithSummary("Rank a sweep's contingencies"),
		forge.WithDescription(
			"Worst first, with contingencies the screen could not evaluate ahead "+
				"of everything else. A radial outage that islands part of the "+
				"network is usually the most consequential thing on the list and "+
				"is exactly what a linear factor cannot answer — ranking it by a "+
				"loading it does not have would bury it."),
		forge.WithTags("Contingency"),
		forge.WithRequestSchema(rankedRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Ranked candidates", rankedResponse{}),
		forge.WithResponseSchema(http.StatusNotFound, "No such run", nil),
	))

	return firstErr
}

// ListRuns returns every recorded sweep of one network.
func (c *Controller) ListRuns(ctx forge.Context) error {
	var req listRunsRequest
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

	return ctx.JSON(http.StatusOK, runsResponse{Runs: found})
}

// GetRun returns one recorded sweep.
func (c *Controller) GetRun(ctx forge.Context) error {
	var req getRunRequest
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

// GetRanked returns a sweep's contingencies worst first.
func (c *Controller) GetRanked(ctx forge.Context) error {
	var req rankedRequest
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

	candidates := found.Concerning()
	if req.All {
		candidates = found.Ranked()
	}

	unevaluated := 0

	for _, candidate := range found.Candidates {
		if !candidate.Screenability.Evaluated() {
			unevaluated++
		}
	}

	return ctx.JSON(http.StatusOK, rankedResponse{
		RunID:       found.ID.String(),
		Candidates:  candidates,
		Unevaluated: unevaluated,
	})
}
