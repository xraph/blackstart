package weather

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/xraph/fabriq"

	"github.com/xraph/forge"
	"github.com/xraph/vessel"

	"github.com/xraph/blackstart/extensions/datasource"
	dsfeed "github.com/xraph/blackstart/extensions/datasource/feed"
	"github.com/xraph/blackstart/extensions/fabric"
	"github.com/xraph/blackstart/extensions/geo"
	"github.com/xraph/blackstart/extensions/provenance"
	"github.com/xraph/blackstart/extensions/weather/fetch"
	"github.com/xraph/blackstart/extensions/weather/store"
	fabriqstore "github.com/xraph/blackstart/extensions/weather/store/fabriq"
	"github.com/xraph/blackstart/pkg/apiauth"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/hooks"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/solver"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

// ExtensionName is how this extension is identified to Forge.
const ExtensionName = "weather"

// Extension hosts the weather model in the Forge application.
type Extension struct {
	*forge.BaseExtension

	store  store.Store
	engine *Engine

	// poll configures the ingest loop. Disabled by default: the server runs
	// it and bsctl does not, which is a property of which binary is running
	// rather than of this extension.
	poll PollConfig

	// Worker lifecycle, guarded by the same mutex.
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// Option configures the extension.
type Option func(*Extension)

// WithStore supplies a store, overriding the Fabriq-backed default.
func WithStore(s store.Store) Option {
	return func(e *Extension) { e.store = s }
}

// WithPolling configures the ingest loop.
func WithPolling(config PollConfig) Option {
	return func(e *Extension) { e.poll = config }
}

// New builds the extension.
func New(opts ...Option) *Extension {
	e := &Extension{
		BaseExtension: forge.NewBaseExtension(
			ExtensionName, "0.1.0",
			"Owns what the atmosphere was doing, and when we learned it",
		),
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Dependencies declares what must start first.
func (e *Extension) Dependencies() []string {
	return []string{fabric.ExtensionName}
}

// Register provides the engine into the container.
func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	return vessel.Provide(app.Container(), func() (*Engine, error) {
		if e.engine == nil {
			return nil, errors.New(ErrResolvedTooEarly)
		}

		return e.engine, nil
	}, vessel.WithAliases(ExtensionName))
}

// ErrResolvedTooEarly is what the container returns when something asks for the
// engine before this extension has started.
const ErrResolvedTooEarly = "weather: the engine is not built yet; something " +
	"resolved it before the " + ExtensionName + " extension started, which means " +
	"its Dependencies() does not name " + ExtensionName

// Start builds the engine.
func (e *Extension) Start(context.Context) error {
	app := e.App()
	if app == nil {
		return errors.New("weather: started without an application")
	}

	if e.store == nil {
		fab, err := forge.Inject[*fabriq.Fabriq](app.Container())
		if err != nil {
			return fmt.Errorf(
				"weather: resolving the data fabric: %w; the fabric extension "+
					"must start first, which its Dependencies() declares", err)
		}

		fabricExt, err := forge.Inject[*fabric.Extension](app.Container())
		if err != nil {
			return fmt.Errorf("weather: resolving the fabric extension: %w", err)
		}

		built, err := fabriqstore.New(fab, fabricExt.Tenant())
		if err != nil {
			return err
		}

		e.store = built
	}

	bus, _ := forge.Inject[*hooks.Bus](app.Container())

	engine, err := NewEngine(e.store, bus, e.halves(app)...)
	if err != nil {
		return err
	}

	e.engine = engine

	// Registered here rather than in Register, because the controller needs
	// the engine and the engine needs the store.
	//
	// Optional guard: an unauthenticated deployment provides none, and a nil
	// guard declares no middleware. A configured one that cannot resolve its
	// middleware refuses every request rather than serving it open.
	guard, _ := forge.Inject[*apiauth.Guard](app.Container())

	if err := app.RegisterController(NewController(engine, guard)); err != nil {
		return fmt.Errorf("weather: registering the HTTP controller: %w", err)
	}

	return nil
}

// halves assembles whichever of the ingest and sampling halves this
// deployment can support.
//
// All of it optional, and separately so. A deployment with no datasource
// registry can still serve reads; one with no grid service can still retrieve
// and sample later from the stored bytes. Demanding everything up front would
// make the read API depend on the solver being up, which it does not.
func (e *Extension) halves(app forge.App) []EngineOption {
	var opts []EngineOption

	datasourceExt, dsErr := forge.Inject[*datasource.Engine](app.Container())
	recorder, recErr := forge.Inject[provenance.Recorder](app.Container())

	if dsErr == nil && recErr == nil {
		opts = append(opts, WithIngest(
			&registryFeeds{engine: datasourceExt},
			fetch.New(nil),
			recorder))
	}

	geoEngine, geoErr := forge.Inject[*geo.Engine](app.Container())
	grids, gridErr := forge.Inject[*solver.GridClient](app.Container())

	if geoErr == nil && gridErr == nil {
		opts = append(opts, WithSampling(geoEngine, grids))
	}

	return opts
}

// registryFeeds adapts the datasource engine to what ingest asks of it.
//
// The engine has no "feed by slug within a source", because a feed slug is
// unique within its source rather than globally — so the lookup is a filtered
// list and a match, which belongs here rather than in the ingest path.
type registryFeeds struct {
	engine *datasource.Engine
}

func (r *registryFeeds) GetSourceBySlug(
	ctx context.Context, slug string,
) (id.ID, error) {
	found, err := r.engine.GetSourceBySlug(ctx, slug)
	if err != nil {
		return id.Nil, err
	}

	return found.ID, nil
}

func (r *registryFeeds) FeedBySlug(
	ctx context.Context, sourceSlug, feedSlug string,
) (dsfeed.Feed, error) {
	source, err := r.engine.GetSourceBySlug(ctx, sourceSlug)
	if err != nil {
		return dsfeed.Feed{}, err
	}

	feeds, err := r.engine.ListFeeds(ctx,
		dsfeed.Filter{SourceID: source.ID}, pkgstore.Page{Limit: pkgstore.MaxPageLimit})
	if err != nil {
		return dsfeed.Feed{}, err
	}

	for _, found := range feeds {
		if found.Slug == feedSlug {
			return found, nil
		}
	}

	return dsfeed.Feed{}, fmt.Errorf(
		"%w: source %q has no feed %q", bserr.ErrNotFound, sourceSlug, feedSlug)
}

// Stop releases the store.
func (e *Extension) Stop(context.Context) error {
	e.engine = nil

	if e.store == nil {
		return nil
	}

	return e.store.Close()
}

// Health reports whether the store is reachable.
func (e *Extension) Health(ctx context.Context) error {
	if e.store == nil {
		return errors.New("weather: no store; the extension has not started")
	}

	return e.store.Ping(ctx)
}

// Engine returns the built engine, or nil before Start.
func (e *Extension) Engine() *Engine { return e.engine }
