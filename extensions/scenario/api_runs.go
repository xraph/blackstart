package scenario

// The run half of the HTTP surface. Split from api.go by domain object, the
// way store/memory splits its composite from its run accessor: a scenario is
// authored and a run is executed. The Controller, its guard and its
// compile-time assertion live in api.go, and so does bound — the limit rule
// is one rule for both lists.

import (
	"net/http"

	"github.com/xraph/forge"

	scnid "github.com/xraph/blackstart/extensions/scenario/id"
	scn "github.com/xraph/blackstart/extensions/scenario/scenario"
	"github.com/xraph/blackstart/pkg/httperr"
)

// --- Request DTOs ---

type enqueueScenarioRunRequest struct {
	ID string `path:"id" json:"-" description:"Scenario identifier (scn_…)"`
}

type getScenarioRunRequest struct {
	ID string `path:"id" description:"Run identifier (srun_…)"`
}

type listScenarioRunsRequest struct {
	ID    string `path:"id"     description:"Scenario identifier (scn_…)"`
	Limit int    `query:"limit" description:"Maximum results, most recent first" optional:"true"`
}

type listRecentRunsRequest struct {
	Limit int `query:"limit" description:"Maximum results, most recent first" optional:"true"`
}

// --- Response DTOs ---

// scenarioRunsResponse, not runsResponse. `runsResponse` has already collided
// twice in this repository, because Forge keys an OpenAPI component by bare
// Go type name and the label passed to WithResponseSchema is not it.
//
// Both run listings answer with this one type rather than a second wrapper of
// the same shape, and that is the same rule read from the other end. A
// distinct `recentRunsResponse` holding one `runs` field would put a second,
// byte-identical component in the specification and a second, byte-identical
// type in every generated client — two names for one shape, which is the
// collision problem inverted and just as much work for a reader to undo. The
// endpoints differ in which runs they select, not in what a run list is.
type scenarioRunsResponse struct {
	Runs []scn.ScenarioRun `json:"runs"`
}

// --- Routes ---

// runRoutes registers the run endpoints. Called by Routes in api.go, which
// owns the controller and the order routes are declared in.
func (c *Controller) runRoutes(router forge.Router) error {
	var firstErr error

	check := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	check(router.POST("/scenarios/:id/runs", c.Enqueue,
		forge.WithName("scenario.runs.enqueue"),
		forge.WithSummary("Queue a run of a scenario"),
		forge.WithDescription(
			"Freezes a copy of the scenario's manifest into a new run and returns "+
				"it queued. A worker claims and executes it; poll the run for its "+
				"status and, once complete, the sweep and cascade it produced."),
		forge.WithTags("Scenario"),
		forge.WithRequestSchema(enqueueScenarioRunRequest{}),
		forge.WithResponseSchema(http.StatusAccepted, "ScenarioRun", scn.ScenarioRun{}),
		forge.WithResponseSchema(http.StatusNotFound, "No such scenario", nil),
	))

	check(router.GET("/scenarios/:id/runs", c.ListRuns,
		forge.WithName("scenario.runs.list"),
		forge.WithSummary("List a scenario's runs"),
		forge.WithDescription("Every recorded run of one scenario, most recent first."),
		forge.WithTags("Scenario"),
		forge.WithRequestSchema(listScenarioRunsRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Runs", scenarioRunsResponse{}),
	))

	// Before /runs/:id, so the literal segment is what a reader meets first.
	// Not a routing requirement — Forge matches a static segment ahead of a
	// parameter either way — but the two paths differ by one segment and the
	// order they are declared in is the only thing telling a reader they are
	// two endpoints rather than one with an optional id.
	check(router.GET("/runs", c.ListRecentRuns,
		forge.WithName("scenario.runs.recent"),
		forge.WithSummary("List runs across every scenario"),
		forge.WithDescription(
			"Every recorded run, most recent first, whatever scenario it "+
				"belongs to. This is what a run index reads; a single "+
				"scenario's history is /scenarios/{id}/runs."),
		forge.WithTags("Scenario"),
		forge.WithRequestSchema(listRecentRunsRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Runs", scenarioRunsResponse{}),
	))

	check(router.GET("/runs/:id", c.GetRun,
		forge.WithName("scenario.runs.get"),
		forge.WithSummary("Get a scenario run"),
		forge.WithDescription(
			"One run: the manifest it froze, where it is in its lifecycle, the "+
				"step it failed at if it failed, and the sweep and cascade it "+
				"produced if it completed."),
		forge.WithTags("Scenario"),
		forge.WithRequestSchema(getScenarioRunRequest{}),
		forge.WithResponseSchema(http.StatusOK, "ScenarioRun", scn.ScenarioRun{}),
		forge.WithResponseSchema(http.StatusNotFound, "No such run", nil),
	))

	return firstErr
}

// Enqueue queues a run of one scenario.
//
// 202 rather than 200: nothing has been computed when this answers. The run
// exists, it is queued, and a worker will claim it — reporting 200 would say
// the request was fulfilled, when what was fulfilled is the recording of an
// intention.
func (c *Controller) Enqueue(ctx forge.Context) error {
	var req enqueueScenarioRunRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	want, err := scnid.ParseScenario(req.ID)
	if err != nil {
		return forge.BadRequest(err.Error())
	}

	queued, err := c.engine.Enqueue(ctx.Request().Context(), want)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusAccepted, queued)
}

// ListRuns returns one scenario's runs, most recent first.
func (c *Controller) ListRuns(ctx forge.Context) error {
	var req listScenarioRunsRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	want, err := scnid.ParseScenario(req.ID)
	if err != nil {
		return forge.BadRequest(err.Error())
	}

	found, err := c.engine.Runs(ctx.Request().Context(), want, bound(req.Limit))
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, scenarioRunsResponse{Runs: found})
}

// ListRecentRuns returns runs across every scenario, most recent first.
//
// bound(), the same call ListRuns and List make. The store honors a
// non-positive limit as "everything" and this endpoint is unfiltered, so it is
// the one place on this surface where forwarding an absent limit would page
// every run the tenant has ever recorded — which is the request the console's
// run index makes on every load.
func (c *Controller) ListRecentRuns(ctx forge.Context) error {
	var req listRecentRunsRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	found, err := c.engine.RecentRuns(ctx.Request().Context(), bound(req.Limit))
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, scenarioRunsResponse{Runs: found})
}

// GetRun returns one run.
func (c *Controller) GetRun(ctx forge.Context) error {
	var req getScenarioRunRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	want, err := scnid.ParseRun(req.ID)
	if err != nil {
		return forge.BadRequest(err.Error())
	}

	found, err := c.engine.Run(ctx.Request().Context(), want)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, found)
}
