package provenance

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/vessel"

	"github.com/xraph/blackstart/extensions/provenance/store"
	"github.com/xraph/blackstart/extensions/provenance/store/memory"
	"github.com/xraph/blackstart/pkg/apiauth"
	"github.com/xraph/blackstart/pkg/hooks"
)

// ExtensionName is how this extension is identified to Forge and how other
// extensions declare a dependency on it.
const ExtensionName = "provenance"

// Extension hosts the provenance recorder in the Forge application.
type Extension struct {
	*forge.BaseExtension

	store  store.Store
	engine *Engine
}

// Option configures the extension.
type Option func(*Extension)

// WithStore supplies a store, overriding the in-memory default.
//
// The default is in-memory for now, and that is a gap rather than a design:
// provenance is durable data by definition — a chain that vanishes on restart
// answers "what produced this number" only until the next deploy. The Fabriq
// store lands with the network model, which is the first thing whose
// provenance has to outlive a process.
func WithStore(s store.Store) Option {
	return func(e *Extension) { e.store = s }
}

// New builds the extension.
func New(opts ...Option) *Extension {
	e := &Extension{
		BaseExtension: forge.NewBaseExtension(
			ExtensionName, "0.1.0",
			"Records what produced every number: dataset version, producer version, code commit",
		),
		store: memory.New(),
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Dependencies declares nothing.
//
// The recorder holds its own store and publishes an interface. Extensions that
// record provenance depend on the Recorder interface rather than on this
// extension's startup order.
func (e *Extension) Dependencies() []string { return nil }

// Register provides the engine and the Recorder interface into the container.
//
// Both, because other extensions should take the narrow Recorder — an
// extension that records a run has no business rewriting the provenance graph
// — while this extension's own controller needs the full engine.
func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	if err := vessel.Provide(app.Container(), func() (*Engine, error) {
		if e.engine == nil {
			return nil, errors.New(ErrResolvedTooEarly)
		}

		return e.engine, nil
	}, vessel.WithAliases(ExtensionName)); err != nil {
		return err
	}

	return vessel.Provide(app.Container(), func() (Recorder, error) {
		if e.engine == nil {
			return nil, errors.New(ErrResolvedTooEarly)
		}

		return e.engine, nil
	})
}

// ErrResolvedTooEarly is what the container returns when something asks for the
// engine before this extension has started.
//
// A named constant rather than an inline string so the test asserts the message
// a caller actually sees. A test holding its own copy would keep passing
// through any rewording, which is the same as not testing it.
const ErrResolvedTooEarly = "provenance: the recorder is not built yet; something " +
	"resolved it before the " + ExtensionName + " extension started, which means " +
	"its Dependencies() does not name " + ExtensionName

// Start builds the engine and mounts the HTTP surface.
func (e *Extension) Start(ctx context.Context) error {
	app := e.App()
	if app == nil {
		return errors.New("provenance: started without an application")
	}

	// The bus is optional: a host that publishes no domain events is a
	// legitimate configuration, and bsctl is one.
	bus, _ := forge.Inject[*hooks.Bus](app.Container())

	engine, err := NewEngine(e.store, bus)
	if err != nil {
		return err
	}

	e.engine = engine

	// Optional: an unauthenticated deployment provides no guard, and a nil
	// guard declares no middleware. A configured one that cannot resolve its
	// middleware refuses every request rather than serving it open.
	guard, _ := forge.Inject[*apiauth.Guard](app.Container())

	if err := app.RegisterController(NewController(engine, guard)); err != nil {
		return fmt.Errorf("provenance: registering the HTTP controller: %w", err)
	}

	return nil
}

// Stop releases the store.
func (e *Extension) Stop(context.Context) error {
	e.engine = nil

	return e.store.Close()
}

// Health reports whether the store is reachable.
func (e *Extension) Health(ctx context.Context) error {
	return e.store.Ping(ctx)
}

// Engine returns the built engine, or nil before Start.
func (e *Extension) Engine() *Engine { return e.engine }
