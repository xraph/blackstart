package scl

import (
	"net/http"

	"github.com/xraph/forge"

	"github.com/xraph/blackstart/pkg/apiauth"

	"github.com/xraph/blackstart/extensions/scl/device"
	sclid "github.com/xraph/blackstart/extensions/scl/id"
	"github.com/xraph/blackstart/pkg/httperr"
)

// --- Request DTOs ---

type listDevicesRequest struct {
	Limit int `query:"limit" description:"Maximum results" optional:"true"`
}

type deviceRequest struct {
	ID string `path:"id" description:"Device identifier (ied_…)"`
}

// --- Response DTOs ---

type devicesResponse struct {
	Devices []device.IED `json:"devices"`
}

// impactResponse is what stops working if a device is lost.
//
// The two directions are named rather than both called "subscriptions",
// because they answer different questions and a caller reading a stored
// response has no other way to tell which it is holding.
type impactResponse struct {
	Device device.IED `json:"device"`

	// Listeners subscribe to this device. If it is lost, their logic stops
	// receiving — the question the electrical model cannot answer at all.
	Listeners []device.Subscription `json:"listeners"`

	// ListensTo is what this device subscribes to.
	ListensTo []device.Subscription `json:"listens_to"`

	// Publications are the streams it publishes, with their wire addresses.
	Publications []device.Publication `json:"publications"`

	// Unaddressed counts control blocks with no network address. Valid in a
	// device template and a misconfiguration in a commissioned substation, so
	// the count is reported and the judgement is left to the reader.
	Unaddressed int `json:"unaddressed"`
}

// --- Controller ---

// Controller exposes the communication model over HTTP.
//
// Read-only. Configuration arrives by importing an SCL file, where the source
// identifiers and the provenance chain are recorded; an endpoint accepting a
// device inline would produce a communication graph with no file behind it,
// and nothing downstream could say where an edge came from.
type Controller struct {
	engine *Engine
	guard  *apiauth.Guard
}

// NewController builds the HTTP surface over an engine.
func NewController(engine *Engine, guard *apiauth.Guard) *Controller {
	return &Controller{engine: engine, guard: guard}
}

// Name implements forge.Controller.
func (c *Controller) Name() string { return "scl:controller" }

// Prefix returns the base path for SCL routes.
func (c *Controller) Prefix() string { return "/api/v1/scl" }

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

	check(router.GET("/devices", c.ListDevices,
		forge.WithName("scl.devices.list"),
		forge.WithSummary("List configured devices"),
		forge.WithDescription(
			"Every intelligent electronic device an imported configuration "+
				"describes, by the name every external reference resolves against."),
		forge.WithTags("SCL"),
		forge.WithRequestSchema(listDevicesRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Devices", devicesResponse{}),
	))

	check(router.GET("/devices/:id/impact", c.Impact,
		forge.WithName("scl.devices.impact"),
		forge.WithSummary("What stops working if this device is lost"),
		forge.WithDescription(
			"The communication graph around one device: who subscribes to it, "+
				"what it subscribes to, and what it publishes. The first is the "+
				"question the electrical model cannot answer at all — two relays "+
				"on the same bus may exchange nothing, and two relays in "+
				"different substations may be tightly coupled.\n\n"+
				"Advisory. Nothing here commands anything; it reports what a "+
				"configuration file says."),
		forge.WithTags("SCL"),
		forge.WithRequestSchema(deviceRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Impact", impactResponse{}),
		forge.WithResponseSchema(http.StatusNotFound, "No such device", nil),
	))

	return firstErr
}

// ListDevices returns every configured device.
func (c *Controller) ListDevices(ctx forge.Context) error {
	var req listDevicesRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	found, err := c.engine.Devices(ctx.Request().Context(), req.Limit)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, devicesResponse{Devices: found})
}

// Impact returns the communication graph around one device.
func (c *Controller) Impact(ctx forge.Context) error {
	var req deviceRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	want, err := sclid.ParseIED(req.ID)
	if err != nil {
		return forge.BadRequest(err.Error())
	}

	inner := ctx.Request().Context()

	found, err := c.engine.DeviceByID(inner, want)
	if err != nil {
		return httperr.From(err)
	}

	listeners, err := c.engine.Listeners(inner, want)
	if err != nil {
		return httperr.From(err)
	}

	listensTo, err := c.engine.Listens(inner, want)
	if err != nil {
		return httperr.From(err)
	}

	publications, err := c.engine.Publications(inner, want)
	if err != nil {
		return httperr.From(err)
	}

	out := impactResponse{
		Device:       found,
		Listeners:    listeners,
		ListensTo:    listensTo,
		Publications: publications,
	}

	for _, publication := range publications {
		if publication.MAC == "" {
			out.Unaddressed++
		}
	}

	return ctx.JSON(http.StatusOK, out)
}
