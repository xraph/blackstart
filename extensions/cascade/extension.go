package cascade

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/vessel"

	"github.com/xraph/blackstart/extensions/cascade/store"
	"github.com/xraph/blackstart/extensions/cascade/store/memory"
	"github.com/xraph/blackstart/extensions/fabric"
	"github.com/xraph/blackstart/pkg/apiauth"
	"github.com/xraph/blackstart/pkg/hooks"
	"github.com/xraph/blackstart/pkg/solver"
)

// ExtensionName is how this extension is identified to Forge and how other
// extensions declare a dependency on it.
const ExtensionName = "cascade"

// Extension hosts the cascade engine in the Forge application.
type Extension struct {
	*forge.BaseExtension

	store  store.Store
	engine *Engine
}

// Option configures the extension.
type Option func(*Extension)

// WithStore supplies a store, overriding the in-memory default.
//
// The default is in-memory for the same reason contingency's is: a run is
// derived data and can be recomputed from the model that produced it, so
// losing it on restart costs a re-simulation rather than a fact. Unlike
// contingency, there is currently no Fabriq-backed implementation to fall
// back to — a caller wanting durability supplies its own store.Store.
func WithStore(s store.Store) Option {
	return func(e *Extension) { e.store = s }
}

// New builds the extension.
func New(opts ...Option) *Extension {
	e := &Extension{
		BaseExtension: forge.NewBaseExtension(
			ExtensionName, "0.1.0",
			"Runs cascade simulations against the solver service and records what it found",
		),
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Dependencies declares what must start first.
//
// The fabric extension, for the same reason contingency names it: a store
// built for this extension in the future would borrow its Fabriq facade, and
// naming the dependency now means the ordering is already correct when that
// store is added rather than something to rediscover then.
func (e *Extension) Dependencies() []string { return []string{fabric.ExtensionName} }

// Register provides the engine into the container.
//
// The constructor closes over the extension rather than over a value, because
// the engine does not exist until Start — it needs the solver connection.
// Resolving before then is a wiring error and says so rather than handing back
// a nil.
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

// ErrResolvedTooEarly is what the container returns when something asks for
// the engine before this extension has started.
//
// A named constant rather than an inline string so the test asserts the
// message a caller actually sees. A test holding its own copy would keep
// passing through any rewording, which is the same as not testing it.
const ErrResolvedTooEarly = "cascade: the engine is not built yet; something " +
	"resolved it before the " + ExtensionName + " extension started, which means " +
	"its Dependencies() does not name " + ExtensionName

// Start builds the engine.
func (e *Extension) Start(ctx context.Context) error {
	app := e.App()
	if app == nil {
		return errors.New("cascade: started without an application")
	}

	// No Fabriq-backed store exists for this extension yet, so the default
	// stays in-memory rather than resolving the fabric the way contingency's
	// Start does — there is nothing there for it to build against.
	if e.store == nil {
		e.store = memory.New()
	}

	client, err := forge.Inject[*solver.Client](app.Container())
	if err != nil {
		return fmt.Errorf("cascade: resolving the solver client: %w", err)
	}

	// The bus is optional. A host that publishes no domain events is a
	// legitimate configuration — bsctl is one.
	bus, _ := forge.Inject[*hooks.Bus](app.Container())

	engine, err := NewEngine(e.store, client, bus)
	if err != nil {
		return err
	}

	e.engine = engine

	// Registered here rather than in Register, because the controller needs
	// the engine and the engine needs the solver connection. Forge accepts a
	// controller after startup; it would not accept a nil engine behind one.
	// Optional: an unauthenticated deployment provides no guard, and a nil
	// guard declares no middleware. A configured one that cannot resolve its
	// middleware refuses every request rather than serving it open.
	guard, _ := forge.Inject[*apiauth.Guard](app.Container())

	if err := app.RegisterController(NewController(engine, guard)); err != nil {
		return fmt.Errorf("cascade: registering the HTTP controller: %w", err)
	}

	return nil
}

// Stop releases the store.
func (e *Extension) Stop(context.Context) error {
	e.engine = nil

	// Nil until Start builds it, and stopping an extension that never started
	// is a normal path — a host that fails partway through startup stops
	// everything it registered.
	if e.store == nil {
		return nil
	}

	return e.store.Close()
}

// Health reports whether the store is reachable.
//
// The solver is deliberately not probed. It is a separate process with its
// own health, and reporting unhealthy because a solver was restarting would
// take the application down for something the client recovers from on the
// next call.
func (e *Extension) Health(ctx context.Context) error {
	return e.store.Ping(ctx)
}

// Engine returns the built engine, or nil before Start.
func (e *Extension) Engine() *Engine { return e.engine }
