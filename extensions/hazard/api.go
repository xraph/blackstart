package hazard

import (
	"net/http"
	"time"

	"github.com/xraph/forge"

	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/hazard/conductor"
	"github.com/xraph/blackstart/extensions/hazard/store"
	"github.com/xraph/blackstart/pkg/apiauth"
	"github.com/xraph/blackstart/pkg/httperr"
	"github.com/xraph/blackstart/pkg/id"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

// --- Request DTOs ---

type listRequest struct {
	Limit int    `query:"limit" description:"Maximum results"                   optional:"true"`
	After string `query:"after" description:"Return rows after this identifier" optional:"true"`
}

type spanValuesRequest struct {
	ID     string `path:"id"      description:"Span identifier (span_…)"`
	Issued string `query:"issued" description:"Forecast issuance, RFC 3339"`
	From   string `query:"from"   description:"Window start, RFC 3339"`
	To     string `query:"to"     description:"Window end, RFC 3339, exclusive"`
}

// --- Response DTOs ---

type conductorsResponse struct {
	Conductors []conductor.Conductor `json:"conductors"`
}

type assignmentsResponse struct {
	Assignments []conductor.Assignment `json:"assignments"`
}

type computationsResponse struct {
	Computations []store.Computation `json:"computations"`
}

// valuesResponse is a span's derived numbers over a window.
//
// The issuance is stated once at the top rather than repeated on every value.
// A day of hourly ratings comes from one forecast run, and echoing its
// timestamp twenty-four times would make the link look per-value when it is
// a property of the whole series.
type valuesResponse struct {
	SpanID string `json:"span_id"`

	// IssuedAt is the forecast run these were computed from. Always stated,
	// because "what is this line's rating" is meaningless without it.
	IssuedAt time.Time `json:"issued_at"`

	Values []store.SpanValue `json:"values"`
}

// --- Controller ---

// Controller exposes what the weather is doing to the conductors.
//
// Read-only, for the reason weather's and geo's are: a hazard value is
// produced by a computation that records its provenance, and an endpoint
// accepting one inline would produce a number nothing could trace back to a
// model version or a forecast — the one thing ADR 0006 exists to prevent.
//
// Assignments are the apparent exception and are not one: they are written
// through the CLI, which is where the operational act of surveying a line
// belongs, and a survey is an assertion about the world rather than a derived
// number.
type Controller struct {
	engine *Engine
	guard  *apiauth.Guard
}

// NewController builds the HTTP surface over an engine.
func NewController(engine *Engine, guard *apiauth.Guard) *Controller {
	return &Controller{engine: engine, guard: guard}
}

// Name implements forge.Controller.
func (c *Controller) Name() string { return "hazard:controller" }

// Prefix returns the base path for hazard routes.
func (c *Controller) Prefix() string { return "/api/v1/hazard" }

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

	check(router.GET("/conductors", c.ListConductors,
		forge.WithName("hazard.conductors.list"),
		forge.WithSummary("The conductor library"),
		forge.WithDescription(
			"Standard ACSR types with the properties IEEE 738 needs: diameter, "+
				"resistance at two temperatures, emissivity, absorptivity and "+
				"the maximum operating temperature a rating is the current at."),
		forge.WithTags("Hazard"),
		forge.WithResponseSchema(http.StatusOK, "Conductors", conductorsResponse{}),
	))

	check(router.GET("/assignments", c.ListAssignments,
		forge.WithName("hazard.assignments.list"),
		forge.WithSummary("Which conductor each carrier is strung with"),
		forge.WithDescription(
			"Every assignment carries a basis. A surveyed assignment and one "+
				"assumed from the voltage class are different claims, and a "+
				"rating conditioned on the second is indicative rather than "+
				"authoritative."),
		forge.WithTags("Hazard"),
		forge.WithRequestSchema(listRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Assignments", assignmentsResponse{}),
	))

	check(router.GET("/computations", c.ListComputations,
		forge.WithName("hazard.computations.list"),
		forge.WithSummary("Passes of the models over a weather cycle, newest first"),
		forge.WithDescription(
			"Each names the cycle it read and the provenance run it produced, "+
				"and counts the spans it rated against the spans it skipped for "+
				"want of a conductor assignment."),
		forge.WithTags("Hazard"),
		forge.WithRequestSchema(listRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Computations", computationsResponse{}),
	))

	check(router.GET("/spans/:id/rating", c.GetSpanRating,
		forge.WithName("hazard.spans.rating"),
		forge.WithSummary("A span's thermal rating over a window"),
		forge.WithDescription(
			"Amperes, from IEEE 738's steady-state heat balance evaluated at "+
				"the conductor's temperature limit. Issuance is a parameter "+
				"rather than inferred, because \"what is the rating now\" and "+
				"\"what did the 06Z run say the rating would be\" are different "+
				"questions, and only the second can be checked afterwards."),
		forge.WithTags("Hazard"),
		forge.WithRequestSchema(spanValuesRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Ratings", valuesResponse{}),
	))

	check(router.GET("/spans/:id/loading", c.GetSpanLoading,
		forge.WithName("hazard.spans.loading"),
		forge.WithSummary("A span's mechanical loading over a window"),
		forge.WithDescription(
			"Wind drag and ice weight in newtons per meter, ice thickness in "+
				"meters, ice mass in kilograms per meter, and the resultant of "+
				"drag and weight. Absent quantities are absent rather than zero: "+
				"GFS publishes no freezing-rain accumulation, so a GFS-derived "+
				"pass has no ice series at all."),
		forge.WithTags("Hazard"),
		forge.WithRequestSchema(spanValuesRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Loading", valuesResponse{}),
	))

	return firstErr
}

// ListConductors returns the library.
func (c *Controller) ListConductors(ctx forge.Context) error {
	return ctx.JSON(http.StatusOK, conductorsResponse{Conductors: conductor.Library()})
}

// ListAssignments returns branch-to-conductor bindings.
func (c *Controller) ListAssignments(ctx forge.Context) error {
	page, err := c.page(ctx)
	if err != nil {
		return err
	}

	found, err := c.engine.Assignments(ctx.Request().Context(), page)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, assignmentsResponse{Assignments: found})
}

// ListComputations returns passes of the models.
func (c *Controller) ListComputations(ctx forge.Context) error {
	page, err := c.page(ctx)
	if err != nil {
		return err
	}

	found, err := c.engine.Computations(ctx.Request().Context(), page)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, computationsResponse{Computations: found})
}

// GetSpanRating returns a span's ampacity over a window.
func (c *Controller) GetSpanRating(ctx forge.Context) error {
	return c.spanValues(ctx, []store.Quantity{store.QuantityAmpacity})
}

// GetSpanLoading returns a span's mechanical quantities over a window.
func (c *Controller) GetSpanLoading(ctx forge.Context) error {
	return c.spanValues(ctx, []store.Quantity{
		store.QuantityWindLoad,
		store.QuantityIceThickness,
		store.QuantityIceLoad,
		store.QuantityCombinedLoad,
	})
}

// spanValues serves both span endpoints, which differ only in which series
// they ask for.
func (c *Controller) spanValues(ctx forge.Context, quantities []store.Quantity) error {
	var req spanValuesRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	span, err := gid.ParseSpan(req.ID)
	if err != nil {
		return forge.BadRequest(err.Error())
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

	got, err := c.engine.SpanValues(
		ctx.Request().Context(), span, quantities, issued, from, to)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, valuesResponse{
		SpanID:   span.String(),
		IssuedAt: issued,
		Values:   got,
	})
}

// page reads the pagination parameters from a request.
func (c *Controller) page(ctx forge.Context) (pkgstore.Page, error) {
	var req listRequest
	if err := ctx.BindRequest(&req); err != nil {
		return pkgstore.Page{}, forge.BadRequest(err.Error())
	}

	out := pkgstore.Page{Limit: req.Limit}

	if req.After != "" {
		after, err := id.Parse(req.After)
		if err != nil {
			return pkgstore.Page{}, forge.BadRequest(err.Error())
		}

		out.After = after
	}

	return out, nil
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
