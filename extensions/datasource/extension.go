package datasource

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/fabriq"
	"github.com/xraph/forge"
	"github.com/xraph/vessel"

	"github.com/xraph/blackstart/extensions/datasource/plugin"
	"github.com/xraph/blackstart/extensions/datasource/store"
	fabriqstore "github.com/xraph/blackstart/extensions/datasource/store/fabriq"
	"github.com/xraph/blackstart/extensions/fabric"
	"github.com/xraph/blackstart/pkg/hooks"
)

// ExtensionName is how this extension is identified to Forge and how other
// extensions declare a dependency on it.
const ExtensionName = "datasource"

// Extension hosts the public data source registry in the Forge application.
type Extension struct {
	*forge.BaseExtension

	store  store.Store
	engine *Engine
}

// Option configures the extension.
type Option func(*Extension)

// WithStore supplies a store, overriding the Fabriq-backed default.
//
// The registry is durable data, not derived: it records what this project is
// permitted to consume and what was known about each entry when. That is why
// the default here is Fabriq-backed where the solver extensions default to
// memory — losing a solution costs a re-solve, and losing the registry loses
// the provenance chain every number depends on.
func WithStore(s store.Store) Option {
	return func(e *Extension) { e.store = s }
}

// New builds the extension.
func New(opts ...Option) *Extension {
	e := &Extension{
		BaseExtension: forge.NewBaseExtension(
			ExtensionName, "0.1.0",
			"The public data source registry: every dataset blackstart is permitted to consume",
		),
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Dependencies declares that the fabric extension must start first, since the
// default store borrows its Fabriq facade.
//
// Named rather than left to registration order. Fabriq's own forgeext returns
// nil here, which is a silent trap the first time two extensions are
// registered the other way round.
func (e *Extension) Dependencies() []string { return []string{fabric.ExtensionName} }

// Register provides the engine into the container.
//
// The constructor closes over the extension because the engine does not exist
// until Start — it needs the Fabriq facade, which the fabric extension opens in
// its own Start. Resolving before then is a wiring error and says so rather
// than handing back a nil.
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
//
// A named constant rather than an inline string so the test asserts the message
// a caller actually sees. A test holding its own copy would keep passing
// through any rewording, which is the same as not testing it.
const ErrResolvedTooEarly = "datasource: the engine is not built yet; something " +
	"resolved it before the " + ExtensionName + " extension started, which means " +
	"its Dependencies() does not name " + ExtensionName

// Start builds the store and the engine, then mounts the HTTP surface.
func (e *Extension) Start(ctx context.Context) error {
	app := e.App()
	if app == nil {
		return errors.New("datasource: started without an application")
	}

	if e.store == nil {
		fab, err := forge.Inject[*fabriq.Fabriq](app.Container())
		if err != nil {
			return fmt.Errorf(
				"datasource: resolving the data fabric: %w; the fabric extension "+
					"must start first, which its Dependencies() declares", err)
		}

		// The tenant comes from the fabric extension rather than from this
		// one's own configuration. Blackstart is single-tenant, and two places
		// naming the tenant is how a write ends up scoped differently from the
		// read that looks for it.
		fabricExt, err := forge.Inject[*fabric.Extension](app.Container())
		if err != nil {
			return fmt.Errorf("datasource: resolving the fabric extension: %w", err)
		}

		built, err := fabriqstore.New(fab, fabricExt.Tenant())
		if err != nil {
			return err
		}

		e.store = built
	}

	// The bus is optional: a host that publishes no domain events is a
	// legitimate configuration, and bsctl is one.
	bus, _ := forge.Inject[*hooks.Bus](app.Container())

	engine, err := NewEngine(e.store, plugin.NewManager(nil), bus)
	if err != nil {
		return err
	}

	e.engine = engine

	// Registered here rather than in Register, because the controller needs the
	// engine and the engine needs a started fabric. Forge accepts a controller
	// after startup; it would not accept a nil engine behind one.
	if err := app.RegisterController(NewController(engine)); err != nil {
		return fmt.Errorf("datasource: registering the HTTP controller: %w", err)
	}

	return nil
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
		return errors.New("datasource: no store; the extension has not started")
	}

	return e.store.Ping(ctx)
}

// Engine returns the built engine, or nil before Start.
func (e *Extension) Engine() *Engine { return e.engine }
