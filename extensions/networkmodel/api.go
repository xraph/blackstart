package networkmodel

import (
	"net/http"

	"github.com/xraph/forge"

	"github.com/xraph/blackstart/pkg/apiauth"

	nmid "github.com/xraph/blackstart/extensions/networkmodel/id"
	"github.com/xraph/blackstart/extensions/networkmodel/model"
	"github.com/xraph/blackstart/pkg/httperr"
)

// --- Request DTOs ---

type listModelsRequest struct {
	Limit int `query:"limit" description:"Maximum results, most recent first" optional:"true"`
}

type modelRequest struct {
	// Accepts an identifier or a content hash. They name the same thing from
	// two directions: an identifier comes from a previous response, a hash
	// comes from a result somebody is trying to explain. Separate parameters
	// would make the caller work out which kind of string they are holding.
	ID string `path:"id" description:"Model identifier (nmdl_…) or content hash"`
}

// --- Response DTOs ---

type modelsResponse struct {
	Models []model.NetworkModel `json:"models"`
}

// encodingResponse is the canonical bytes a model encodes to.
//
// Base64 rather than an octet stream, so the hash travels in the same document
// as the bytes it names. A caller that fetched them separately could pair a
// hash with an encoding it does not describe, which is precisely the mistake
// content addressing exists to make impossible.
type encodingResponse struct {
	ModelID     string `json:"model_id"`
	ContentHash string `json:"content_hash"`

	// Encoding is the canonical form, base64. What the solver reads, byte for
	// byte — read from storage rather than re-encoded, so what a result was
	// computed from is what comes out.
	Encoding []byte `json:"encoding"`

	SizeBytes int `json:"size_bytes"`
}

// --- Controller ---

// Controller exposes stored network models.
//
// Read-only. Models arrive through the import pipeline, where the dataset
// version, the parser version and the content hash are all recorded together.
// An endpoint accepting a model inline would produce a network with no
// provenance, and every result computed against it would cite a hash that
// leads nowhere.
type Controller struct {
	engine *Engine
	guard  *apiauth.Guard
}

// NewController builds the HTTP surface over an engine.
func NewController(engine *Engine, guard *apiauth.Guard) *Controller {
	return &Controller{engine: engine, guard: guard}
}

// Name implements forge.Controller.
func (c *Controller) Name() string { return "networkmodel:controller" }

// Prefix returns the base path for model routes.
func (c *Controller) Prefix() string { return "/api/v1/models" }

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

	check(router.GET("", c.List,
		forge.WithName("networkmodel.list"),
		forge.WithSummary("List stored network models"),
		forge.WithDescription(
			"Every network this platform can be asked about, most recent "+
				"first. A model is immutable: correcting one produces a new "+
				"model with a new content hash, so a result citing the old hash "+
				"keeps meaning what it meant."),
		forge.WithTags("Network model"),
		forge.WithRequestSchema(listModelsRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Models", modelsResponse{}),
	))

	check(router.GET("/:id", c.Get,
		forge.WithName("networkmodel.get"),
		forge.WithSummary("Get a network model by identifier or content hash"),
		forge.WithTags("Network model"),
		forge.WithRequestSchema(modelRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Model", model.NetworkModel{}),
		forge.WithResponseSchema(http.StatusNotFound, "No such model", nil),
	))

	check(router.GET("/:id/encoding", c.Encoding,
		forge.WithName("networkmodel.encoding"),
		forge.WithSummary("Get a model's canonical encoding"),
		forge.WithDescription(
			"The bytes the solver reads, exactly as stored. The content hash "+
				"is the sha256 of these bytes, so anyone can verify that a "+
				"result citing that hash was computed against this network and "+
				"not another."),
		forge.WithTags("Network model"),
		forge.WithRequestSchema(modelRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Encoding", encodingResponse{}),
		forge.WithResponseSchema(http.StatusNotFound, "No such model", nil),
	))

	return firstErr
}

// List returns stored models.
func (c *Controller) List(ctx forge.Context) error {
	var req listModelsRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	found, err := c.engine.List(ctx.Request().Context(), req.Limit)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, modelsResponse{Models: found})
}

// Get returns one model.
func (c *Controller) Get(ctx forge.Context) error {
	found, err := c.resolve(ctx)
	if err != nil {
		return err
	}

	return ctx.JSON(http.StatusOK, found)
}

// Encoding returns the canonical bytes.
func (c *Controller) Encoding(ctx forge.Context) error {
	found, err := c.resolve(ctx)
	if err != nil {
		return err
	}

	encoded, err := c.engine.Encoding(ctx.Request().Context(), found.ContentHash)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, encodingResponse{
		ModelID:     found.ID.String(),
		ContentHash: found.ContentHash,
		Encoding:    encoded,
		SizeBytes:   len(encoded),
	})
}

// resolve accepts either an identifier or a content hash.
func (c *Controller) resolve(ctx forge.Context) (model.NetworkModel, error) {
	var req modelRequest
	if err := ctx.BindRequest(&req); err != nil {
		return model.NetworkModel{}, forge.BadRequest(err.Error())
	}

	inner := ctx.Request().Context()

	if parsed, err := nmid.ParseModel(req.ID); err == nil {
		found, err := c.engine.Get(inner, parsed)
		if err != nil {
			return model.NetworkModel{}, httperr.From(err)
		}

		return found, nil
	}

	found, err := c.engine.FindByHash(inner, req.ID)
	if err != nil {
		return model.NetworkModel{}, httperr.From(err)
	}

	return found, nil
}
