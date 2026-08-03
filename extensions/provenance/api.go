package provenance

import (
	"net/http"

	"github.com/xraph/forge"

	"github.com/xraph/blackstart/pkg/apiauth"

	pvid "github.com/xraph/blackstart/extensions/provenance/id"
	"github.com/xraph/blackstart/extensions/provenance/record"
	"github.com/xraph/blackstart/pkg/httperr"
)

// --- Request DTOs ---

type getRunRequest struct {
	ID string `path:"id" description:"Run identifier (run_…)"`
}

type getDatasetRequest struct {
	ID string `path:"id" description:"Dataset version identifier (dsv_…)"`
}

type getModelRequest struct {
	ID string `path:"id" description:"Model version identifier (mver_…)"`
}

// --- Response DTOs ---

// traceResponse is the answer to "what produced this number".
//
// One shape carrying the whole chain rather than links a caller follows,
// because the question ADR 0006 exists to make answerable is the whole chain.
// An API that returned a run and made the caller fetch its producer and each
// of its inputs would have made provenance a research task again.
type traceResponse struct {
	Run      record.Run          `json:"run"`
	Producer record.ModelVersion `json:"producer"`
	Inputs   []TracedInput       `json:"inputs"`

	// Reproducible says whether this chain is complete enough to reproduce.
	//
	// Computed and returned rather than left for the caller to infer from
	// which fields are populated. A consumer deciding for itself is a consumer
	// that will decide differently from the next one.
	Reproducible bool `json:"reproducible"`
}

// --- Controller ---

// Controller exposes the provenance chain over HTTP.
//
// Read-only. Provenance is written by whatever produced the thing being traced
// — an import, a solve, a screen — and an endpoint that let a caller assert
// what produced a number would defeat the entire purpose of recording it.
type Controller struct {
	engine *Engine
	guard  *apiauth.Guard
}

// NewController builds the HTTP surface over an engine.
func NewController(engine *Engine, guard *apiauth.Guard) *Controller {
	return &Controller{engine: engine, guard: guard}
}

// Name implements forge.Controller.
func (c *Controller) Name() string { return "provenance:controller" }

// Prefix returns the base path for provenance routes.
func (c *Controller) Prefix() string { return "/api/v1/provenance" }

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

	check(router.GET("/runs/:id", c.GetRun,
		forge.WithName("provenance.runs.trace"),
		forge.WithSummary("Trace a run"),
		forge.WithDescription(
			"What produced this number: the run, the producer version and the "+
				"commit it was built from, and every dataset version it read "+
				"with the role each played. One call rather than a graph walk."),
		forge.WithTags("Provenance"),
		forge.WithRequestSchema(getRunRequest{}),
		forge.WithResponseSchema(http.StatusOK, "The full chain", traceResponse{}),
		forge.WithResponseSchema(http.StatusNotFound, "No such run", nil),
	))

	check(router.GET("/datasets/:id", c.GetDataset,
		forge.WithName("provenance.datasets.get"),
		forge.WithSummary("Get a dataset version"),
		forge.WithDescription(
			"One retrieval of one external dataset: where it came from, the "+
				"sha256 of what arrived, and the parser version that read it."),
		forge.WithTags("Provenance"),
		forge.WithRequestSchema(getDatasetRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Dataset version", record.DatasetVersion{}),
		forge.WithResponseSchema(http.StatusNotFound, "No such dataset version", nil),
	))

	check(router.GET("/models/:id", c.GetModel,
		forge.WithName("provenance.models.get"),
		forge.WithSummary("Get a producer version"),
		forge.WithDescription(
			"A solver build, a parser, or a trained model, with the commit it "+
				"was built from and the parameters that change its answer."),
		forge.WithTags("Provenance"),
		forge.WithRequestSchema(getModelRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Model version", record.ModelVersion{}),
		forge.WithResponseSchema(http.StatusNotFound, "No such model version", nil),
	))

	return firstErr
}

// GetRun returns the full chain behind one run.
func (c *Controller) GetRun(ctx forge.Context) error {
	var req getRunRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	want, err := pvid.ParseRun(req.ID)
	if err != nil {
		return forge.BadRequest(err.Error())
	}

	trace, err := c.engine.Trace(ctx.Request().Context(), want)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, traceResponse{
		Run:          trace.Run,
		Producer:     trace.Producer,
		Inputs:       trace.Inputs,
		Reproducible: trace.Reproducible(),
	})
}

// GetDataset returns one retrieval.
func (c *Controller) GetDataset(ctx forge.Context) error {
	var req getDatasetRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	want, err := pvid.ParseDataset(req.ID)
	if err != nil {
		return forge.BadRequest(err.Error())
	}

	found, err := c.engine.GetDataset(ctx.Request().Context(), want)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, found)
}

// GetModel returns one producer version.
func (c *Controller) GetModel(ctx forge.Context) error {
	var req getModelRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	want, err := pvid.ParseModel(req.ID)
	if err != nil {
		return forge.BadRequest(err.Error())
	}

	found, err := c.engine.GetModel(ctx.Request().Context(), want)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, found)
}
