package risk

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/xraph/forge"

	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/risk/store"
	"github.com/xraph/blackstart/pkg/apiauth"
	"github.com/xraph/blackstart/pkg/httperr"
	"github.com/xraph/blackstart/pkg/id"
)

// --- Request DTOs ---

type componentSeriesRequest struct {
	ID     string `path:"id"      description:"Component identifier (span_… or strc_…)"`
	Issued string `query:"issued" description:"Forecast issuance, RFC 3339"`
	From   string `query:"from"   description:"Window start, RFC 3339"`
	To     string `query:"to"     description:"Window end, RFC 3339, exclusive"`
}

// --- Response DTOs ---

// seriesResponse is a component's probability series over a window.
//
// The issuance is stated once at the top rather than repeated on every value,
// matching extensions/hazard/api.go's valuesResponse: a day of hourly
// probabilities comes from one computation pass, and echoing its timestamp on
// every row would make the link look per-value when it is a property of the
// whole series.
type seriesResponse struct {
	ComponentID string `json:"component_id"`

	// IssuedAt is the computation pass these were produced by. Always stated,
	// because "what is this component's probability" is meaningless without
	// knowing which weather cycle it was computed from.
	IssuedAt time.Time `json:"issued_at"`

	Values []store.ComponentValue `json:"values"`
}

// --- Controller ---

// Controller exposes what the risk models computed.
//
// Read-only, for the reason hazard's own is: a probability is produced by a
// computation that records its provenance, and an endpoint accepting one
// inline would produce a number nothing could trace back to a model version
// or the weather cycle it scored.
//
// There is no endpoint to look up a computation by its ComputationID.
// ComputeResult's own doc (compute.go) is explicit that the identifier is
// minted for the caller to log, not stored anywhere this extension could
// resolve it from — there is no computation table, only the value rows a
// pass wrote. A route that always answered 404 would be worse than no route,
// so this diverges from that reading of the brief; the provenance run a pass
// records (ComputeResult.RunID) is already traceable through the existing
// GET /api/v1/provenance/runs/:id.
type Controller struct {
	engine *Engine
	guard  *apiauth.Guard
}

// NewController builds the HTTP surface over an engine.
func NewController(engine *Engine, guard *apiauth.Guard) *Controller {
	return &Controller{engine: engine, guard: guard}
}

// Name implements forge.Controller.
func (c *Controller) Name() string { return "risk:controller" }

// Prefix returns the base path for risk routes.
func (c *Controller) Prefix() string { return "/api/v1/risk" }

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

	check(router.GET("/components/:id/series", c.GetComponentSeries,
		forge.WithName("risk.components.series"),
		forge.WithSummary("A component's failure probability over a window"),
		forge.WithDescription(
			"Per-hour P(failure): a span's conductor probability or a "+
				"structure's, whichever the identifier names. Issuance is a "+
				"parameter rather than inferred, the same reason hazard's span "+
				"endpoints require it — a window this API guessed would give "+
				"the same request a different answer tomorrow."),
		forge.WithTags("Risk"),
		forge.WithRequestSchema(componentSeriesRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Probabilities", seriesResponse{}),
	))

	return firstErr
}

// GetComponentSeries returns one component's probability series.
func (c *Controller) GetComponentSeries(ctx forge.Context) error {
	var req componentSeriesRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	component, err := parseComponent(req.ID)
	if err != nil {
		return err
	}

	issued, err := parseInstant("issued", req.Issued)
	if err != nil {
		return err
	}

	from, err := parseInstant("from", req.From)
	if err != nil {
		return err
	}

	to, err := parseInstant("to", req.To)
	if err != nil {
		return err
	}

	got, err := c.engine.ComponentSeries(
		ctx.Request().Context(), component, nil, issued, from, to)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, seriesResponse{
		ComponentID: component.String(),
		IssuedAt:    issued,
		Values:      got,
	})
}

// parseComponent reads a span or a structure identifier, rejecting anything
// else. ComponentID is only ever one of the two (store.go's own doc), so
// accepting a bare id.Parse here would let a route or a circuit identifier
// through to a query that can never match it.
func parseComponent(raw string) (id.ID, error) {
	if span, err := gid.ParseSpan(raw); err == nil {
		return span, nil
	}

	if structure, err := gid.ParseStructure(raw); err == nil {
		return structure, nil
	}

	return id.Nil, forge.BadRequest(
		raw + " is not a span or structure identifier (span_… or strc_…)")
}

// parseInstant reads an RFC 3339 timestamp from a query parameter.
//
// Required rather than defaulted. A window this API guessed would return a
// different answer tomorrow for the same request, which is the opposite of
// what a reproducibility-first platform should do.
func parseInstant(name, raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, forge.BadRequest(
			name + " is required, as an RFC 3339 timestamp; a window this API " +
				"chose would give the same request a different answer tomorrow")
	}

	at, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, forge.BadRequest(
			name + " must be an RFC 3339 timestamp: " + err.Error())
	}

	return at.UTC(), nil
}

// ComponentSeries reads one component's probability series over a window.
//
// Component-generic rather than split into a span endpoint and a structure
// endpoint the way hazard splits rating from loading: a risk ComponentID
// already carries what kind of thing it is in its own prefix, and asking for
// every quantity this extension writes costs nothing extra, because a given
// component only ever has values under the one quantity its Compute pass
// wrote for it (conductor_failure for a span, structure_failure for a
// structure — never both).
//
// Passing no quantities reads every one this extension knows, matching
// hazard's own SpanValues. The issuance is required rather than defaulted to
// the newest, for the same reason: a window this method chose would give the
// same request a different answer tomorrow.
func (e *Engine) ComponentSeries(
	ctx context.Context,
	component id.ID, quantities []store.Quantity, issuedAt, from, to time.Time,
) ([]store.ComponentValue, error) {
	if component.IsZero() {
		return nil, errors.New("risk: reading a series needs a component")
	}

	if issuedAt.IsZero() {
		return nil, errors.New(
			"risk: reading a series needs an issuance; a probability is only " +
				"meaningful alongside the computation pass that produced it")
	}

	if len(quantities) == 0 {
		quantities = store.Quantities()
	}

	return e.store.Values().ForComponent(ctx, component, quantities, issuedAt, from, to)
}
