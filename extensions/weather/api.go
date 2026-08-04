package weather

import (
	"net/http"
	"time"

	"github.com/xraph/forge"

	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/weather/field"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	wid "github.com/xraph/blackstart/extensions/weather/id"
	"github.com/xraph/blackstart/extensions/weather/observation"
	"github.com/xraph/blackstart/extensions/weather/store"
	"github.com/xraph/blackstart/pkg/apiauth"
	"github.com/xraph/blackstart/pkg/httperr"
	"github.com/xraph/blackstart/pkg/id"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

// --- Request DTOs ---

type listCyclesRequest struct {
	Model  string `query:"model"  description:"Restrict to one model: hrrr or gfs" optional:"true"`
	Status string `query:"status" description:"Restrict to one processing stage"   optional:"true"`
	Limit  int    `query:"limit"  description:"Maximum results"                    optional:"true"`
}

type cycleRequest struct {
	ID string `path:"id" description:"Cycle identifier (wxcyc_…)"`
}

type spanForecastRequest struct {
	ID     string `path:"id"      description:"Span identifier (span_…)"`
	Field  string `query:"field"  description:"Field name; omit for every field" optional:"true"`
	Issued string `query:"issued" description:"Issuance, RFC 3339"`
	From   string `query:"from"   description:"Window start, RFC 3339"`
	To     string `query:"to"     description:"Window end, RFC 3339, exclusive"`
}

type stationObservationsRequest struct {
	ID    string `path:"id"     description:"Station identifier (wxstn_…)"`
	Field string `query:"field" description:"Field name; omit for every field" optional:"true"`
	From  string `query:"from"  description:"Window start, RFC 3339"`
	To    string `query:"to"    description:"Window end, RFC 3339, exclusive"`
}

// --- Response DTOs ---

type cyclesResponse struct {
	Cycles []forecast.Cycle `json:"cycles"`
}

type gridsResponse struct {
	CycleID string          `json:"cycle_id"`
	Grids   []forecast.Grid `json:"grids"`
}

// forecastResponse is a span's weather, with what produced it.
//
// The provenance is grouped rather than repeated on every sample. A day of
// hourly values for one field is one retrieval, and echoing its identifier
// twenty-four times would make the link look per-value when it is not.
type forecastResponse struct {
	SpanID string `json:"span_id"`

	// IssuedAt is the run these samples came from. Always stated, because
	// "what does the forecast say" is meaningless without it.
	IssuedAt time.Time `json:"issued_at"`

	Samples []forecast.Sample `json:"samples"`
}

type observationsResponse struct {
	StationID string                    `json:"station_id"`
	Readings  []observation.Observation `json:"readings"`
}

// --- Controller ---

// Controller exposes what the atmosphere was doing.
//
// Read-only. Samples arrive through the ingest pipeline, where the retrieval
// that produced them is recorded; an endpoint accepting them inline would
// produce numbers nothing could trace back to a dataset version, which is the
// one thing ADR 0006 exists to prevent.
type Controller struct {
	engine *Engine
	guard  *apiauth.Guard
}

// NewController builds the HTTP surface over an engine.
func NewController(engine *Engine, guard *apiauth.Guard) *Controller {
	return &Controller{engine: engine, guard: guard}
}

// Name implements forge.Controller.
func (c *Controller) Name() string { return "weather:controller" }

// Prefix returns the base path for weather routes.
func (c *Controller) Prefix() string { return "/api/v1/weather" }

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

	check(router.GET("/cycles", c.ListCycles,
		forge.WithName("weather.cycles.list"),
		forge.WithSummary("List model runs, newest issuance first"),
		forge.WithTags("Weather"),
		forge.WithRequestSchema(listCyclesRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Cycles", cyclesResponse{}),
	))

	check(router.GET("/cycles/:id", c.GetCycle,
		forge.WithName("weather.cycles.get"),
		forge.WithSummary("Get one model run"),
		forge.WithTags("Weather"),
		forge.WithRequestSchema(cycleRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Cycle", forecast.Cycle{}),
		forge.WithResponseSchema(http.StatusNotFound, "No such cycle", nil),
	))

	check(router.GET("/cycles/:id/grids", c.GetGrids,
		forge.WithName("weather.cycles.grids"),
		forge.WithSummary("List a run's retrievals"),
		forge.WithDescription(
			"One entry per field per forecast hour, each naming the bytes it "+
				"was decoded from and the dataset version that recorded the "+
				"retrieval. Covered and total say how many spans fell inside "+
				"the grid's domain — HRRR is CONUS-only, so a span outside it "+
				"has no sample rather than a zero."),
		forge.WithTags("Weather"),
		forge.WithRequestSchema(cycleRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Grids", gridsResponse{}),
		forge.WithResponseSchema(http.StatusNotFound, "No such cycle", nil),
	))

	check(router.GET("/spans/:id/forecast", c.GetSpanForecast,
		forge.WithName("weather.spans.forecast"),
		forge.WithSummary("What a run expected at one span"),
		forge.WithDescription(
			"Issuance is a parameter rather than inferred from the window, "+
				"because \"what does the latest run say\" and \"what did the 06Z "+
				"run say\" are different questions — and a reader who cannot ask "+
				"the second cannot check a forecast against what happened."),
		forge.WithTags("Weather"),
		forge.WithRequestSchema(spanForecastRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Forecast", forecastResponse{}),
	))

	check(router.GET("/stations/:id/observations", c.GetStationObservations,
		forge.WithName("weather.stations.observations"),
		forge.WithSummary("What a station measured"),
		forge.WithTags("Weather"),
		forge.WithRequestSchema(stationObservationsRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Observations", observationsResponse{}),
	))

	return firstErr
}

// ListCycles returns model runs.
func (c *Controller) ListCycles(ctx forge.Context) error {
	var req listCyclesRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	found, err := c.engine.Cycles(ctx.Request().Context(),
		store.CycleFilter{
			Model:  forecast.Model(req.Model),
			Status: forecast.CycleStatus(req.Status),
		},
		pkgstore.Page{Limit: req.Limit})
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, cyclesResponse{Cycles: found})
}

// GetCycle returns one model run.
func (c *Controller) GetCycle(ctx forge.Context) error {
	want, err := c.cycle(ctx)
	if err != nil {
		return err
	}

	found, err := c.engine.Cycle(ctx.Request().Context(), want)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, found)
}

// GetGrids returns a run's retrievals.
func (c *Controller) GetGrids(ctx forge.Context) error {
	want, err := c.cycle(ctx)
	if err != nil {
		return err
	}

	inner := ctx.Request().Context()

	if _, err := c.engine.Cycle(inner, want); err != nil {
		return httperr.From(err)
	}

	found, err := c.engine.Grids(inner, want)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, gridsResponse{
		CycleID: want.String(),
		Grids:   found,
	})
}

// GetSpanForecast returns what a run expected at one span.
func (c *Controller) GetSpanForecast(ctx forge.Context) error {
	var req spanForecastRequest
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

	fields, err := parseFields(req.Field)
	if err != nil {
		return err
	}

	got, err := c.engine.SpanForecast(
		ctx.Request().Context(), span, fields, issued, from, to)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, forecastResponse{
		SpanID:   span.String(),
		IssuedAt: issued,
		Samples:  got,
	})
}

// GetStationObservations returns what a station measured.
func (c *Controller) GetStationObservations(ctx forge.Context) error {
	var req stationObservationsRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	station, err := wid.ParseStation(req.ID)
	if err != nil {
		return forge.BadRequest(err.Error())
	}

	from, err := parseInstant("from", req.From)
	if err != nil {
		return err
	}

	to, err := parseInstant("to", req.To)
	if err != nil {
		return err
	}

	fields, err := parseFields(req.Field)
	if err != nil {
		return err
	}

	got, err := c.engine.StationObservations(
		ctx.Request().Context(), station, fields, from, to)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, observationsResponse{
		StationID: station.String(),
		Readings:  got,
	})
}

// cycle reads and parses the cycle identifier from a request.
func (c *Controller) cycle(ctx forge.Context) (id.ID, error) {
	var req cycleRequest
	if err := ctx.BindRequest(&req); err != nil {
		return id.Nil, forge.BadRequest(err.Error())
	}

	parsed, err := wid.ParseCycle(req.ID)
	if err != nil {
		return id.Nil, forge.BadRequest(err.Error())
	}

	return parsed, nil
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

// parseFields reads an optional field name, defaulting to every field.
func parseFields(raw string) ([]field.Field, error) {
	if raw == "" {
		return nil, nil
	}

	want := field.Field(raw)
	if !want.Valid() {
		return nil, forge.BadRequest(
			"unknown field " + raw + "; the vocabulary is closed so that a " +
				"misspelling cannot read back as an empty series")
	}

	return []field.Field{want}, nil
}
