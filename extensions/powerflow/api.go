package powerflow

import (
	"net/http"

	"github.com/xraph/forge"

	pfid "github.com/xraph/blackstart/extensions/powerflow/id"
	"github.com/xraph/blackstart/extensions/powerflow/solution"
	"github.com/xraph/blackstart/pkg/httperr"
)

// --- Request DTOs ---

type getSolutionRequest struct {
	ID string `path:"id" description:"Solution identifier (pfsol_…)"`
}

type listSolutionsRequest struct {
	Hash  string `query:"modelHash" description:"Content hash of the network model"`
	Limit int    `query:"limit"     description:"Maximum results, most recent first" optional:"true"`
}

type violationsRequest struct {
	ID string `path:"id" description:"Solution identifier (pfsol_…)"`
	// A threshold rather than a fixed 1.0, because an emergency rating differs
	// from a long-term one and this API has no standing to decide which
	// applies.
	Threshold float64 `query:"threshold" description:"Loading above which a branch is reported. Zero means 1.0" optional:"true"`
}

// --- Response DTOs ---

type solutionsResponse struct {
	Solutions []solution.Solution `json:"solutions"`
}

// violationsResponse names the overloaded branches by index.
//
// Indices rather than names, because this extension holds no network model and
// inventing branch names here would mean two sources of truth for what a
// branch is called. The caller has the model it submitted.
type violationsResponse struct {
	SolutionID string  `json:"solution_id"`
	Threshold  float64 `json:"threshold"`
	Branches   []int   `json:"branches"`
}

// --- Controller ---

// Controller exposes recorded power-flow solutions over HTTP.
//
// Read-only. Running a solve is not a GET, and it is not a POST here either:
// a solve needs a network model, and models arrive through `bsctl` and the
// import pipeline where the provenance chain is recorded alongside them.
// An endpoint that accepted a model inline would produce numbers with no
// dataset version behind them, which is the one thing ADR 0006 forbids.
type Controller struct {
	engine *Engine
}

// NewController builds the HTTP surface over an engine.
func NewController(engine *Engine) *Controller {
	return &Controller{engine: engine}
}

// Name implements forge.Controller.
func (c *Controller) Name() string { return "powerflow:controller" }

// Prefix returns the base path for power-flow routes.
func (c *Controller) Prefix() string { return "/api/v1/powerflow" }

// Middlewares returns per-controller middleware.
func (c *Controller) Middlewares() []forge.Middleware { return nil }

// Routes registers the read endpoints.
func (c *Controller) Routes(router forge.Router) error {
	var firstErr error

	check := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	check(router.GET("/solutions", c.ListSolutions,
		forge.WithName("powerflow.solutions.list"),
		forge.WithSummary("List solutions for a network"),
		forge.WithDescription(
			"Every recorded solve of one network model, most recent first. The "+
				"model is named by content hash, so two solves of the same "+
				"network are found together whatever it was called."),
		forge.WithTags("Power flow"),
		forge.WithRequestSchema(listSolutionsRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Solutions", solutionsResponse{}),
		forge.WithResponseSchema(http.StatusBadRequest, "No model hash given", nil),
	))

	check(router.GET("/solutions/:id", c.GetSolution,
		forge.WithName("powerflow.solutions.get"),
		forge.WithSummary("Get a solution"),
		forge.WithDescription(
			"One solved power flow, with the solver version and model hash that "+
				"produced it and whether reactive limits were enforced."),
		forge.WithTags("Power flow"),
		forge.WithRequestSchema(getSolutionRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Solution", solution.Solution{}),
		forge.WithResponseSchema(http.StatusNotFound, "No such solution", nil),
	))

	check(router.GET("/solutions/:id/violations", c.GetViolations,
		forge.WithName("powerflow.solutions.violations"),
		forge.WithSummary("Branches over their rating"),
		forge.WithDescription(
			"Branch indices loaded above a threshold, worst first. Unrated "+
				"branches are never reported: a zero rating means the limit is "+
				"unknown, not zero."),
		forge.WithTags("Power flow"),
		forge.WithRequestSchema(violationsRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Violations", violationsResponse{}),
		forge.WithResponseSchema(http.StatusNotFound, "No such solution", nil),
	))

	return firstErr
}

// ListSolutions returns every recorded solve of one network.
func (c *Controller) ListSolutions(ctx forge.Context) error {
	var req listSolutionsRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	if req.Hash == "" {
		return forge.BadRequest(
			"modelHash is required; listing every solution across every network " +
				"is not a question anyone asks and would page forever")
	}

	found, err := c.engine.ListByModel(ctx.Request().Context(), req.Hash, req.Limit)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, solutionsResponse{Solutions: found})
}

// GetSolution returns one recorded solve.
func (c *Controller) GetSolution(ctx forge.Context) error {
	var req getSolutionRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	want, err := pfid.ParseSolution(req.ID)
	if err != nil {
		return forge.BadRequest(err.Error())
	}

	found, err := c.engine.Get(ctx.Request().Context(), want)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, found)
}

// GetViolations returns the branches loaded past a threshold.
func (c *Controller) GetViolations(ctx forge.Context) error {
	var req violationsRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	want, err := pfid.ParseSolution(req.ID)
	if err != nil {
		return forge.BadRequest(err.Error())
	}

	found, err := c.engine.Get(ctx.Request().Context(), want)
	if err != nil {
		return httperr.From(err)
	}

	threshold := req.Threshold
	if threshold <= 0 {
		threshold = 1.0
	}

	return ctx.JSON(http.StatusOK, violationsResponse{
		SolutionID: found.ID.String(),
		Threshold:  threshold,
		Branches:   found.Violations(threshold),
	})
}
