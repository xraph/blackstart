package topology

import (
	"fmt"
	"net/http"

	"github.com/xraph/forge"

	tid "github.com/xraph/blackstart/extensions/topology/id"
	"github.com/xraph/blackstart/extensions/topology/network"
	"github.com/xraph/blackstart/extensions/topology/reduce"
	"github.com/xraph/blackstart/pkg/httperr"
)

// --- Request DTOs ---

type listSubstationsRequest struct {
	Limit int `query:"limit" description:"Maximum results" optional:"true"`
}

type getSubstationRequest struct {
	ID string `path:"id" description:"Substation identifier (sub_…)"`
}

type reduceRequest struct {
	ID string `path:"id" description:"Substation identifier (sub_…)"`

	// Open and Closed are repeated query parameters rather than a request
	// body, so a reduction under a hypothetical switch position is a URL
	// somebody can paste into a report. A GET also says plainly that this
	// stores nothing — the reduction is derived, never a second source of
	// truth.
	Open   []string `query:"open"   description:"Equipment to treat as open (equip_…)"     optional:"true"`
	Closed []string `query:"closed" description:"Equipment to treat as closed (equip_…)"   optional:"true"`
}

// --- Response DTOs ---

type substationsResponse struct {
	Substations []network.Substation `json:"substations"`
}

// reductionResponse is the bus-branch model derived from a substation.
//
// The overrides that produced it are echoed back. A reduction is only
// meaningful alongside the switch positions it assumed, and a caller reading a
// stored response would otherwise have no way to tell which question it
// answers.
type reductionResponse struct {
	SubstationID string           `json:"substation_id"`
	Reduction    reduce.Reduction `json:"reduction"`
	Overrides    map[string]bool  `json:"overrides,omitempty"`
}

// --- Controller ---

// Controller exposes the network model over HTTP.
//
// Read-only. Substations arrive through importers, where the source
// identifiers and the provenance chain are recorded; an endpoint accepting a
// substation inline would produce topology with no dataset version behind it,
// and every reduction derived from it would be unciteable.
type Controller struct {
	engine *Engine
}

// NewController builds the HTTP surface over an engine.
func NewController(engine *Engine) *Controller {
	return &Controller{engine: engine}
}

// Name implements forge.Controller.
func (c *Controller) Name() string { return "topology:controller" }

// Prefix returns the base path for topology routes.
func (c *Controller) Prefix() string { return "/api/v1/topology" }

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

	check(router.GET("/substations", c.ListSubstations,
		forge.WithName("topology.substations.list"),
		forge.WithSummary("List substations"),
		forge.WithDescription("Every site in the network model."),
		forge.WithTags("Topology"),
		forge.WithRequestSchema(listSubstationsRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Substations", substationsResponse{}),
	))

	check(router.GET("/substations/:id", c.GetSubstation,
		forge.WithName("topology.substations.get"),
		forge.WithSummary("Get a substation"),
		forge.WithTags("Topology"),
		forge.WithRequestSchema(getSubstationRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Substation", network.Substation{}),
		forge.WithResponseSchema(http.StatusNotFound, "No such substation", nil),
	))

	check(router.GET("/substations/:id/reduction", c.GetReduction,
		forge.WithName("topology.substations.reduction"),
		forge.WithSummary("Reduce a substation to bus-branch form"),
		forge.WithDescription(
			"Collapses every set of connectivity nodes joined by closed "+
				"zero-impedance devices into one bus. Switch positions come from "+
				"each device's recorded normal state; open and closed override "+
				"individual devices, which is how a study asks what would happen "+
				"if one breaker moved. Derived on every call and never stored — "+
				"two models of one network drift, and the drift stays invisible "+
				"until a result disagrees with a diagram."),
		forge.WithTags("Topology"),
		forge.WithRequestSchema(reduceRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Reduction", reductionResponse{}),
		forge.WithResponseSchema(http.StatusBadRequest, "Unknown equipment named", nil),
		forge.WithResponseSchema(http.StatusNotFound, "No such substation", nil),
	))

	return firstErr
}

// ListSubstations returns every site.
func (c *Controller) ListSubstations(ctx forge.Context) error {
	var req listSubstationsRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	found, err := c.engine.Substations(ctx.Request().Context(), req.Limit)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, substationsResponse{Substations: found})
}

// GetSubstation returns one site.
func (c *Controller) GetSubstation(ctx forge.Context) error {
	var req getSubstationRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	want, err := tid.ParseSubstation(req.ID)
	if err != nil {
		return forge.BadRequest(err.Error())
	}

	found, err := c.engine.Substation(ctx.Request().Context(), want)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, found)
}

// GetReduction derives the bus-branch model.
func (c *Controller) GetReduction(ctx forge.Context) error {
	var req reduceRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	want, err := tid.ParseSubstation(req.ID)
	if err != nil {
		return forge.BadRequest(err.Error())
	}

	overrides, err := parseOverrides(req)
	if err != nil {
		return forge.BadRequest(err.Error())
	}

	reduced, err := c.engine.Reduce(ctx.Request().Context(), want, overrides)
	if err != nil {
		return httperr.From(err)
	}

	echoed := make(map[string]bool, len(overrides))
	for equipment, closed := range overrides {
		echoed[equipment.String()] = closed
	}

	return ctx.JSON(http.StatusOK, reductionResponse{
		SubstationID: want.String(),
		Reduction:    reduced,
		Overrides:    echoed,
	})
}

// parseOverrides reads the open and closed parameters into switch positions.
//
// Naming one device in both is refused rather than resolved by an order the
// caller cannot see. Whichever way it were resolved, half of the callers who
// made the mistake would get the opposite of what they asked for.
func parseOverrides(req reduceRequest) (Overrides, error) {
	out := Overrides{}

	for closed, listed := range map[bool][]string{false: req.Open, true: req.Closed} {
		for _, raw := range listed {
			equipment, err := tid.ParseEquipment(raw)
			if err != nil {
				return nil, err
			}

			if previous, seen := out[equipment]; seen && previous != closed {
				return nil, fmt.Errorf(
					"%s is named as both open and closed; whichever way that were "+
						"resolved, half the callers who wrote it would get the "+
						"opposite of what they asked for", equipment)
			}

			out[equipment] = closed
		}
	}

	return out, nil
}
