package contingency

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/fabriq"

	"github.com/xraph/forge"
	"github.com/xraph/vessel"

	"github.com/xraph/blackstart/extensions/contingency/store"
	fabriqstore "github.com/xraph/blackstart/extensions/contingency/store/fabriq"
	"github.com/xraph/blackstart/extensions/fabric"
	"github.com/xraph/blackstart/pkg/hooks"
	"github.com/xraph/blackstart/pkg/solver"
)

// ExtensionName is how this extension is identified to Forge and how other
// extensions declare a dependency on it.
const ExtensionName = "contingency"

// DefaultSolverVersion is recorded when the host does not supply one.
//
// A placeholder, and a visible one. A run has to name the solver that produced
// it or its findings cannot be reproduced, so this string is deliberately
// conspicuous in a stored record rather than something that reads like a real
// version — a run labeled "unconfigured" prompts the question; one labeled
// "0.0.0" does not.
const DefaultSolverVersion = "unconfigured"

// Extension hosts the contingency engine in the Forge application.
type Extension struct {
	*forge.BaseExtension

	store         store.Store
	solverVersion string
	engine        *Engine
}

// Option configures the extension.
type Option func(*Extension)

// WithStore supplies a store, overriding the in-memory default.
//
// The default is in-memory rather than Fabriq-backed for the same reason the
// power-flow extension's is: a run is derived data and can be recomputed from
// the model that produced it, so losing it on restart costs a re-screen rather
// than a fact.
func WithStore(s store.Store) Option {
	return func(e *Extension) { e.store = s }
}

// WithSolverVersion records which solver produced a run.
//
// Supplied by the host because the screen stream does not carry it: the
// contract puts version fields on the solve response, since repeating them
// across thousands of streamed results would be noise.
// An empty version leaves the default rather than overwriting it. A host that
// has not configured one passes empty, and letting that through would replace
// the conspicuous placeholder with a value NewEngine rejects — turning an
// unconfigured field into a startup failure for every command, including the
// ones that never screen anything.
func WithSolverVersion(version string) Option {
	return func(e *Extension) {
		if version != "" {
			e.solverVersion = version
		}
	}
}

// New builds the extension.
func New(opts ...Option) *Extension {
	e := &Extension{
		BaseExtension: forge.NewBaseExtension(
			ExtensionName, "0.1.0",
			"Screens N-1 contingencies against the solver service and records what it found",
		),
		solverVersion: DefaultSolverVersion,
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Dependencies declares nothing.
//
// This extension reaches the solver over gRPC and holds its results in its own
// store, so it borrows no other extension's connection.
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
const ErrResolvedTooEarly = "contingency: the engine is not built yet; something " +
	"resolved it before the " + ExtensionName + " extension started, which means " +
	"its Dependencies() does not name " + ExtensionName

// Start builds the engine.
func (e *Extension) Start(ctx context.Context) error {
	app := e.App()
	if app == nil {
		return errors.New("contingency: started without an application")
	}

	if e.store == nil {
		fab, err := forge.Inject[*fabriq.Fabriq](app.Container())
		if err != nil {
			return fmt.Errorf(
				"contingency: resolving the data fabric: %w; the fabric extension must "+
					"start first, which its Dependencies() declares", err)
		}

		fabricExt, err := forge.Inject[*fabric.Extension](app.Container())
		if err != nil {
			return fmt.Errorf("contingency: resolving the fabric extension: %w", err)
		}

		built, err := fabriqstore.New(fab, fabricExt.Tenant())
		if err != nil {
			return err
		}

		e.store = built
	}

	client, err := forge.Inject[*solver.Client](app.Container())
	if err != nil {
		return fmt.Errorf("contingency: resolving the solver client: %w", err)
	}

	// The bus is optional. A host that publishes no domain events is a
	// legitimate configuration — bsctl is one.
	bus, _ := forge.Inject[*hooks.Bus](app.Container())

	engine, err := NewEngine(e.store, client, e.solverVersion, bus)
	if err != nil {
		return err
	}

	e.engine = engine

	// Registered here rather than in Register, because the controller needs the
	// engine and the engine needs the solver connection. Forge accepts a
	// controller after startup; it would not accept a nil engine behind one.
	if err := app.RegisterController(NewController(engine)); err != nil {
		return fmt.Errorf("contingency: registering the HTTP controller: %w", err)
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
// health, and reporting unhealthy because a solver was restarting would take
// the application down for something the client recovers from on the next call.
func (e *Extension) Health(ctx context.Context) error {
	return e.store.Ping(ctx)
}

// Engine returns the built engine, or nil before Start.
func (e *Extension) Engine() *Engine { return e.engine }
