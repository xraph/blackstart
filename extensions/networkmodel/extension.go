package networkmodel

import (
	"context"
	"errors"

	"github.com/xraph/forge"
	"github.com/xraph/vessel"

	"github.com/xraph/blackstart/extensions/networkmodel/store"
	"github.com/xraph/blackstart/extensions/networkmodel/store/memory"
	"github.com/xraph/blackstart/extensions/provenance"
	"github.com/xraph/blackstart/pkg/hooks"
)

// ExtensionName is how this extension is identified to Forge.
const ExtensionName = "networkmodel"

// Extension hosts the model builder in the Forge application.
type Extension struct {
	*forge.BaseExtension

	store  store.Store
	engine *Engine
}

// Option configures the extension.
type Option func(*Extension)

// WithStore supplies a store, overriding the in-memory default.
//
// The default is in-memory and that is a gap, stated rather than hidden: a
// model is what every result cites, so losing them on restart means every
// stored solution names a network nobody can produce. The Fabriq store is the
// next piece of this extension.
func WithStore(s store.Store) Option {
	return func(e *Extension) { e.store = s }
}

// New builds the extension.
func New(opts ...Option) *Extension {
	e := &Extension{
		BaseExtension: forge.NewBaseExtension(
			ExtensionName, "0.1.0",
			"Builds immutable network snapshots and records where they came from",
		),
		store: memory.New(),
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Dependencies declares that provenance must start first, since every import
// records a dataset version through it.
func (e *Extension) Dependencies() []string { return []string{provenance.ExtensionName} }

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
const ErrResolvedTooEarly = "networkmodel: the engine is not built yet; something " +
	"resolved it before the " + ExtensionName + " extension started, which means " +
	"its Dependencies() does not name " + ExtensionName

// Start builds the engine.
func (e *Extension) Start(ctx context.Context) error {
	app := e.App()
	if app == nil {
		return errors.New("networkmodel: started without an application")
	}

	// The recorder is optional at this layer even though Dependencies names
	// provenance: a host may register this extension without that one, and an
	// import that cannot record its chain reports so rather than failing.
	recorder, _ := forge.Inject[provenance.Recorder](app.Container())
	bus, _ := forge.Inject[*hooks.Bus](app.Container())

	engine, err := NewEngine(e.store, recorder, bus)
	if err != nil {
		return err
	}

	e.engine = engine

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
