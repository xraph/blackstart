package scl

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/fabriq"

	"github.com/xraph/forge"
	"github.com/xraph/vessel"

	"github.com/xraph/blackstart/extensions/fabric"
	"github.com/xraph/blackstart/extensions/provenance"
	"github.com/xraph/blackstart/extensions/scl/store"
	fabriqstore "github.com/xraph/blackstart/extensions/scl/store/fabriq"
	"github.com/xraph/blackstart/extensions/topology"
	"github.com/xraph/blackstart/pkg/apiauth"
	"github.com/xraph/blackstart/pkg/hooks"
)

// ExtensionName is how this extension is identified to Forge.
const ExtensionName = "scl"

// Extension hosts the SCL importer in the Forge application.
type Extension struct {
	*forge.BaseExtension

	store  store.Store
	engine *Engine
}

// Option configures the extension.
type Option func(*Extension)

// WithStore supplies a store, overriding the Fabriq-backed default.
func WithStore(s store.Store) Option {
	return func(e *Extension) { e.store = s }
}

// New builds the extension.
func New(opts ...Option) *Extension {
	e := &Extension{
		BaseExtension: forge.NewBaseExtension(
			ExtensionName, "0.1.0",
			"Imports IEC 61850 configuration files as topology and a communication graph",
		),
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Dependencies declares what must start first.
func (e *Extension) Dependencies() []string {
	return []string{fabric.ExtensionName, topology.ExtensionName, provenance.ExtensionName}
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
const ErrResolvedTooEarly = "scl: the engine is not built yet; something " +
	"resolved it before the " + ExtensionName + " extension started, which means " +
	"its Dependencies() does not name " + ExtensionName

// Start builds the engine.
func (e *Extension) Start(context.Context) error {
	app := e.App()
	if app == nil {
		return errors.New("scl: started without an application")
	}

	if e.store == nil {
		fab, err := forge.Inject[*fabriq.Fabriq](app.Container())
		if err != nil {
			return fmt.Errorf(
				"scl: resolving the data fabric: %w; the fabric extension must "+
					"start first, which its Dependencies() declares", err)
		}

		fabricExt, err := forge.Inject[*fabric.Extension](app.Container())
		if err != nil {
			return fmt.Errorf("scl: resolving the fabric extension: %w", err)
		}

		built, err := fabriqstore.New(fab, fabricExt.Tenant())
		if err != nil {
			return err
		}

		e.store = built
	}

	// Optional at this layer even though Dependencies names them: a host may
	// register this extension without either, and an ICD import needs neither
	// a substation to write to nor a provenance store to record into.
	topo, _ := forge.Inject[*topology.Engine](app.Container())
	recorder, _ := forge.Inject[provenance.Recorder](app.Container())
	bus, _ := forge.Inject[*hooks.Bus](app.Container())

	engine, err := NewEngine(e.store, topo, recorder, bus)
	if err != nil {
		return err
	}

	e.engine = engine

	// Registered here rather than in Register, because the controller needs
	// the engine and the engine needs the store. Forge accepts a controller
	// after startup; it would not accept a nil engine behind one.
	// Optional: an unauthenticated deployment provides no guard, and a nil
	// guard declares no middleware. A configured one that cannot resolve its
	// middleware refuses every request rather than serving it open.
	guard, _ := forge.Inject[*apiauth.Guard](app.Container())

	if err := app.RegisterController(NewController(engine, guard)); err != nil {
		return fmt.Errorf("scl: registering the HTTP controller: %w", err)
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
		return errors.New("scl: no store; the extension has not started")
	}

	return e.store.Ping(ctx)
}

// Engine returns the built engine, or nil before Start.
func (e *Extension) Engine() *Engine { return e.engine }
