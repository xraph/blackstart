package datasource

import (
	"context"
	"fmt"
	"net/http"

	"github.com/xraph/forge"

	"github.com/xraph/blackstart/pkg/apiauth"

	"github.com/xraph/blackstart/extensions/datasource/feed"
	dsid "github.com/xraph/blackstart/extensions/datasource/id"
	"github.com/xraph/blackstart/extensions/datasource/source"
	"github.com/xraph/blackstart/pkg/bitemporal"
	"github.com/xraph/blackstart/pkg/httperr"
	"github.com/xraph/blackstart/pkg/id"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

// --- Request DTOs ---
//
// The temporal pair appears on every read, because every read in this platform
// can be asked as of a past moment. Spelling them out per request rather than
// pulling them from a shared embedded struct keeps each endpoint's generated
// OpenAPI schema self-describing — a caller reading the spec should not have to
// follow a Go embed to learn that a query parameter exists.

type listSourcesRequest struct {
	Kind    string `query:"kind"    description:"Restrict to one classification: network, weather, outage, geospatial, protocol" optional:"true"`
	Slug    string `query:"slug"    description:"Match one source exactly by slug"                                               optional:"true"`
	Limit   int    `query:"limit"   description:"Maximum results"                                                                optional:"true"`
	After   string `query:"after"   description:"Return results ordered after this identifier"                                   optional:"true"`
	ValidAt string `query:"validAt" description:"Read the world as it was at this RFC 3339 time"                                 optional:"true"`
	KnownAt string `query:"knownAt" description:"Read using only what was known at this RFC 3339 time"                           optional:"true"`
}

type getSourceRequest struct {
	ID      string `path:"id"       description:"Source identifier (dsrc_…) or slug"`
	ValidAt string `query:"validAt" description:"Read the world as it was at this RFC 3339 time"       optional:"true"`
	KnownAt string `query:"knownAt" description:"Read using only what was known at this RFC 3339 time" optional:"true"`
}

type listFeedsRequest struct {
	ID      string `path:"id"       description:"Source identifier (dsrc_…)"`
	Limit   int    `query:"limit"   description:"Maximum results"                                      optional:"true"`
	After   string `query:"after"   description:"Return results ordered after this identifier"         optional:"true"`
	ValidAt string `query:"validAt" description:"Read the world as it was at this RFC 3339 time"       optional:"true"`
	KnownAt string `query:"knownAt" description:"Read using only what was known at this RFC 3339 time" optional:"true"`
}

type getFeedRequest struct {
	ID      string `path:"id"       description:"Feed identifier (dfeed_…)"`
	ValidAt string `query:"validAt" description:"Read the world as it was at this RFC 3339 time"       optional:"true"`
	KnownAt string `query:"knownAt" description:"Read using only what was known at this RFC 3339 time" optional:"true"`
}

// --- Response DTOs ---

type sourcesResponse struct {
	Sources []source.Source `json:"sources"`
}

type feedsResponse struct {
	Feeds []feed.Feed `json:"feeds"`
}

// --- Controller ---

// Controller exposes the public data source registry over HTTP.
//
// Read-only, deliberately. The registry is the list of datasets this project is
// allowed to consume, and spec §8 makes adding one a decision that needs
// discussion — so entries arrive through migrations and `bsctl`, where the
// change is reviewable in a diff, rather than through an endpoint anyone can
// POST to. Making it writable later is adding handlers; making it read-only
// later would mean taking away an API people already use.
type Controller struct {
	engine *Engine
	guard  *apiauth.Guard
}

// NewController builds the HTTP surface over an engine.
func NewController(engine *Engine, guard *apiauth.Guard) *Controller {
	return &Controller{engine: engine, guard: guard}
}

// Name implements forge.Controller.
func (c *Controller) Name() string { return "datasource:controller" }

// Prefix returns the base path for registry routes.
func (c *Controller) Prefix() string { return "/api/v1/datasources" }

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

// Routes registers the registry endpoints.
func (c *Controller) Routes(router forge.Router) error {
	var firstErr error

	check := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	check(router.GET("", c.ListSources,
		forge.WithName("datasource.sources.list"),
		forge.WithSummary("List data sources"),
		forge.WithDescription(
			"The public data source registry. Every dataset blackstart consumes "+
				"traces to an entry here, and every provenance record points at one."),
		forge.WithTags("Data sources"),
		forge.WithRequestSchema(listSourcesRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Sources", sourcesResponse{}),
		forge.WithResponseSchema(http.StatusBadRequest, "Invalid request", nil),
	))

	check(router.GET("/:id", c.GetSource,
		forge.WithName("datasource.sources.get"),
		forge.WithSummary("Get a data source"),
		forge.WithDescription(
			"Accepts either an identifier or a slug. A caller holding a slug from "+
				"a document and one holding an identifier from an API response are "+
				"asking the same question."),
		forge.WithTags("Data sources"),
		forge.WithRequestSchema(getSourceRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Source", source.Source{}),
		forge.WithResponseSchema(http.StatusNotFound, "No such source", nil),
	))

	check(router.GET("/:id/feeds", c.ListFeeds,
		forge.WithName("datasource.feeds.list"),
		forge.WithSummary("List a source's feeds"),
		forge.WithDescription("The retrievable resources within one data source."),
		forge.WithTags("Data sources"),
		forge.WithRequestSchema(listFeedsRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Feeds", feedsResponse{}),
		forge.WithResponseSchema(http.StatusBadRequest, "Invalid identifier", nil),
	))

	check(router.GET("/feeds/:id", c.GetFeed,
		forge.WithName("datasource.feeds.get"),
		forge.WithSummary("Get a feed"),
		forge.WithTags("Data sources"),
		forge.WithRequestSchema(getFeedRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Feed", feed.Feed{}),
		forge.WithResponseSchema(http.StatusNotFound, "No such feed", nil),
	))

	return firstErr
}

// ListSources returns the registry, filtered and paged.
func (c *Controller) ListSources(ctx forge.Context) error {
	var req listSourcesRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	scoped, err := temporal(ctx, req.ValidAt, req.KnownAt)
	if err != nil {
		return forge.BadRequest(err.Error())
	}

	filter := source.Filter{Kind: source.Kind(req.Kind), Slug: req.Slug}

	paging, err := page(req.Limit, req.After)
	if err != nil {
		return forge.BadRequest(err.Error())
	}

	found, err := c.engine.ListSources(scoped, filter, paging)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, sourcesResponse{Sources: found})
}

// GetSource returns one source by identifier or by slug.
func (c *Controller) GetSource(ctx forge.Context) error {
	var req getSourceRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	scoped, err := temporal(ctx, req.ValidAt, req.KnownAt)
	if err != nil {
		return forge.BadRequest(err.Error())
	}

	// An identifier if it parses as one, a slug otherwise. Trying the
	// identifier first because it is unambiguous: a slug can never look like a
	// TypeID, so a value that parses is one.
	if parsed, parseErr := dsid.ParseSource(req.ID); parseErr == nil {
		found, err := c.engine.GetSource(scoped, parsed)
		if err != nil {
			return httperr.From(err)
		}

		return ctx.JSON(http.StatusOK, found)
	}

	found, err := c.engine.GetSourceBySlug(scoped, req.ID)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, found)
}

// ListFeeds returns the retrievable resources within one source.
func (c *Controller) ListFeeds(ctx forge.Context) error {
	var req listFeedsRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	scoped, err := temporal(ctx, req.ValidAt, req.KnownAt)
	if err != nil {
		return forge.BadRequest(err.Error())
	}

	owner, err := dsid.ParseSource(req.ID)
	if err != nil {
		return forge.BadRequest(err.Error())
	}

	paging, err := page(req.Limit, req.After)
	if err != nil {
		return forge.BadRequest(err.Error())
	}

	found, err := c.engine.ListFeeds(scoped, feed.Filter{SourceID: owner}, paging)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, feedsResponse{Feeds: found})
}

// GetFeed returns one feed.
func (c *Controller) GetFeed(ctx forge.Context) error {
	var req getFeedRequest
	if err := ctx.BindRequest(&req); err != nil {
		return forge.BadRequest(err.Error())
	}

	scoped, err := temporal(ctx, req.ValidAt, req.KnownAt)
	if err != nil {
		return forge.BadRequest(err.Error())
	}

	want, err := dsid.ParseFeed(req.ID)
	if err != nil {
		return forge.BadRequest(err.Error())
	}

	found, err := c.engine.GetFeed(scoped, want)
	if err != nil {
		return httperr.From(err)
	}

	return ctx.JSON(http.StatusOK, found)
}

// temporal puts the requested AsOf into the request context.
//
// This is the HTTP boundary's half of the bitemporal contract. AsOf travels in
// the context rather than in every store method signature — doubling every
// read into List and ListAsOf guarantees someone forgets one — so something
// has to put it there, and doing it in one helper is what stops a new endpoint
// from silently reading the present when the caller asked for the past.
func temporal(ctx forge.Context, validAt, knownAt string) (context.Context, error) {
	asOf, err := bitemporal.ParseAsOf(validAt, knownAt)
	if err != nil {
		return nil, err
	}

	return bitemporal.WithAsOf(ctx.Request().Context(), asOf)
}

// page turns request parameters into a store page.
//
// Cursor paging rather than an offset, because the store is bitemporal: an
// offset into a result set that can gain rows between requests skips records
// silently, and a caller paging through a registry that is being corrected
// underneath them would never know what it missed.
//
// A zero limit is left zero for the store's own default rather than a number
// invented here.
func page(limit int, after string) (pkgstore.Page, error) {
	out := pkgstore.Page{Limit: limit}

	if after == "" {
		return out, nil
	}

	cursor, err := id.Parse(after)
	if err != nil {
		return pkgstore.Page{}, fmt.Errorf("after: %w", err)
	}

	out.After = cursor

	return out, nil
}
