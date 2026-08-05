package scenario

import (
	"net/http"

	"github.com/xraph/forge"

	scnid "github.com/xraph/blackstart/extensions/scenario/id"
	scn "github.com/xraph/blackstart/extensions/scenario/scenario"
	"github.com/xraph/blackstart/pkg/apiauth"
	"github.com/xraph/blackstart/pkg/httperr"
)

// --- Request DTOs ---

// defineScenarioRequest is the body of a definition.
//
// ID is optional and is what makes this an upsert rather than a create,
// matching Engine.Define: naming a scenario that already exists revises it,
// and omitting the field mints a new one. Revising is safe because an edit
// cannot reach a run — Enqueue freezes a copy of the manifest — so what an
// edit changes is what the NEXT run will execute.
type defineScenarioRequest struct {
	// json:"-" rather than left untagged. Path and query fields are bound
	// first and the whole struct is then decoded from the body, so an
	// untagged field is addressable by a body key of the same name; here
	// there is no path field to shadow, and the tag keeps it that way if one
	// is ever added.
	ID       string       `json:"id,omitempty" description:"Scenario identifier (scn_…) to revise; omitted mints a new one"`
	Name     string       `json:"name"         description:"Human-friendly label"`
	Manifest scn.Manifest `json:"manifest"     description:"The reproducible binding: model, forecast, policy and seed"`
}

// --- Response DTOs ---

// --- Controller ---

// Controller exposes scenarios and their runs over HTTP.
//
// This is the first write surface among the solver-backed extensions, and the
// asymmetry with contingency and cascade is deliberate rather than an
// oversight in those two. They are read-only because a sweep or a cascade
// needs a network model, and models arrive through the import pipeline where
// their provenance is recorded — an endpoint accepting one inline would
// produce findings with no dataset version behind them. A scenario is a
// different kind of object: it is a definition a user authors, naming a model
// that has already been imported by the content hash the import gave it. POST
// here creates no data whose provenance is in question; it records which
// already-provenanced things to bind together.
//
// # No unit conversion on this boundary
//
// cascade's api.go converts per-unit figures back to watts before publishing
// them, because a CascadeRun stores them per unit of its own base. Nothing
// here needs the equivalent: a scenario and its run publish identifiers,
// counts, statuses and timestamps, and the only physical quantities the
// pipeline produces live in the sweep and cascade records that those
// extensions publish, in SI, through their own surfaces. If a number with a
// unit is ever added to this surface, it needs the same named conversion
// cascade has — a per-unit figure published raw once already read as 41.87 MW
// where the answer was 4187.
type Controller struct {
	engine *Engine
	guard  *apiauth.Guard
}

// NewController builds the HTTP surface over an engine.
func NewController(engine *Engine, guard *apiauth.Guard) *Controller {
	return &Controller{engine: engine, guard: guard}
}

// Name implements forge.Controller.
func (c *Controller) Name() string { return "scenario:controller" }

// Prefix returns the base path for scenario routes.
func (c *Controller) Prefix() string { return "/api/v1/scenario" }

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

// Routes registers the scenario endpoints.
func (c *Controller) Routes(router forge.Router) error {
	var firstErr error

	check := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	check(router.POST("/scenarios", c.Define,
		forge.WithName("scenario.scenarios.define"),
		forge.WithSummary("Define or revise a scenario"),
		forge.WithDescription(
			"Records the binding a run will execute: a network model by content "+
				"hash, a forecast cycle and the moment within it, a contingency "+
				"policy and a seed. Naming an existing scenario revises it, which "+
				"changes what the next run executes and nothing about any run "+
				"already recorded."),
		forge.WithTags("Scenario"),
		forge.WithRequestSchema(defineScenarioRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Scenario", scn.Scenario{}),
		forge.WithResponseSchema(http.StatusBadRequest, "The manifest is not reproducible", nil),
	))

	return firstErr
}

// Define records a scenario, or revises one that already exists.
//
// 200 rather than 201, because Define is an upsert and the response cannot
// honestly claim creation for the half of the calls that revised something.
// The stored scenario comes back either way, which is what a caller needs:
// the identifier the platform minted, and the times it stamped.
func (c *Controller) Define(ctx forge.Context) error {
	var req defineScenarioRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	in := scn.Scenario{Name: req.Name, Manifest: req.Manifest}

	// Parsed rather than passed through. A caller that mistypes an identifier
	// should be told, not handed a second scenario nobody meant to define —
	// which is what an unparsed string would become the moment Define fails to
	// find it.
	if req.ID != "" {
		want, err := scnid.ParseScenario(req.ID)
		if err != nil {
			return forge.BadRequest(err.Error())
		}

		in.ID = want
	}

	stored, err := c.engine.Define(ctx.Request().Context(), in)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, stored)
}
