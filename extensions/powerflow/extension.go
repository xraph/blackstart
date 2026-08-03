package powerflow

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/fabriq"

	"github.com/xraph/forge"
	"github.com/xraph/vessel"

	"github.com/xraph/blackstart/extensions/fabric"
	"github.com/xraph/blackstart/extensions/powerflow/store"
	fabriqstore "github.com/xraph/blackstart/extensions/powerflow/store/fabriq"
	"github.com/xraph/blackstart/pkg/apiauth"
	"github.com/xraph/blackstart/pkg/hooks"
	"github.com/xraph/blackstart/pkg/solver"
)

// ExtensionName is how this extension is identified to Forge and how other
// extensions declare a dependency on it.
const ExtensionName = "powerflow"

// Extension hosts the power-flow engine in the Forge application.
type Extension struct {
	*forge.BaseExtension

	store  store.Store
	engine *Engine
}

// Option configures the extension.
type Option func(*Extension)

// WithStore supplies a store, overriding the in-memory default.
//
// The default is in-memory rather than Fabriq-backed because a solution is
// derived data: it can always be recomputed from the model that produced it,
// so losing the cache on restart costs a re-solve rather than a fact. The
// Fabriq store arrives when something depends on solutions outliving a
// process, and the seam is here so that change is one line at the call site.
func WithStore(s store.Store) Option {
	return func(e *Extension) { e.store = s }
}

// New builds the extension.
func New(opts ...Option) *Extension {
	e := &Extension{
		BaseExtension: forge.NewBaseExtension(
			ExtensionName, "0.1.0",
			"Runs power flows against the solver service and records the results",
		),
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Dependencies declares nothing.
//
// This extension reaches the solver over gRPC and holds its results in its own
// store, so it borrows no other extension's connection. Naming a dependency it
// does not have would constrain startup order for no reason.
// Dependencies declares what must start first.
//
// The fabric extension, because the default store borrows its Fabriq facade.
// Results are durable rather than in-memory: a number that vanishes when the
// process exits cannot be cited, and every figure this platform produces has
// to be traceable to the inputs behind it.
func (e *Extension) Dependencies() []string { return []string{fabric.ExtensionName} }

// Register provides the engine into the container.
//
// The constructor closes over the extension rather than over a value, because
// the engine does not exist until Start — it needs the solver connection,
// which is not open at registration time. Resolving before then is a wiring
// error and says so rather than handing back a nil.
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
const ErrResolvedTooEarly = "powerflow: the engine is not built yet; something " +
	"resolved it before the " + ExtensionName + " extension started, which means " +
	"its Dependencies() does not name " + ExtensionName

// Start builds the engine.
//
// The solver client and hook bus are resolved rather than constructed, so this
// extension does not decide how the platform connects to the solver — the host
// does, once, and every extension that needs it gets the same connection.
func (e *Extension) Start(ctx context.Context) error {
	app := e.App()
	if app == nil {
		return errors.New("powerflow: started without an application")
	}

	if e.store == nil {
		fab, err := forge.Inject[*fabriq.Fabriq](app.Container())
		if err != nil {
			return fmt.Errorf(
				"powerflow: resolving the data fabric: %w; the fabric extension must "+
					"start first, which its Dependencies() declares", err)
		}

		fabricExt, err := forge.Inject[*fabric.Extension](app.Container())
		if err != nil {
			return fmt.Errorf("powerflow: resolving the fabric extension: %w", err)
		}

		built, err := fabriqstore.New(fab, fabricExt.Tenant())
		if err != nil {
			return err
		}

		e.store = built
	}

	client, err := forge.Inject[*solver.Client](app.Container())
	if err != nil {
		return fmt.Errorf("powerflow: resolving the solver client: %w", err)
	}

	// The bus is optional. A host that publishes no domain events is a
	// legitimate configuration — bsctl is one — and requiring it would make
	// every command depend on infrastructure it has no use for.
	bus, _ := forge.Inject[*hooks.Bus](app.Container())

	engine, err := NewEngine(e.store, client, bus)
	if err != nil {
		return err
	}

	e.engine = engine

	// Registered here rather than in Register, because the controller needs the
	// engine and the engine needs the solver connection. Forge accepts a
	// controller after startup; it would not accept a nil engine behind one.
	// Optional: an unauthenticated deployment provides no guard, and a nil
	// guard declares no middleware. A configured one that cannot resolve its
	// middleware refuses every request rather than serving it open.
	guard, _ := forge.Inject[*apiauth.Guard](app.Container())

	if err := app.RegisterController(NewController(engine, guard)); err != nil {
		return fmt.Errorf("powerflow: registering the HTTP controller: %w", err)
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
// The solver is deliberately not probed. It is a separate process with its own
// health, and a power-flow extension that reported unhealthy because a solver
// was restarting would take the whole application's health down for something
// it recovers from on the next call — the client re-uploads and retries.
func (e *Extension) Health(ctx context.Context) error {
	return e.store.Ping(ctx)
}

// Engine returns the built engine, or nil before Start.
func (e *Extension) Engine() *Engine { return e.engine }
