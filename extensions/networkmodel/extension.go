package networkmodel

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/fabriq"

	"github.com/xraph/forge"
	"github.com/xraph/vessel"

	"github.com/xraph/blackstart/extensions/fabric"
	"github.com/xraph/blackstart/extensions/networkmodel/store"
	fabriqstore "github.com/xraph/blackstart/extensions/networkmodel/store/fabriq"
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

// WithStore supplies a store, overriding the Fabriq-backed default.
//
// The default is durable rather than in-memory, because a model is what every
// result cites: losing them on restart would leave every stored solution
// naming a network nobody can produce. The in-memory store exists for tests
// and for `bsctl` run against a local file, both of which pass it explicitly.
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
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Dependencies declares what must start first.
//
// The fabric extension, because the default store borrows its Fabriq facade,
// and provenance because every import records a dataset version through it.
func (e *Extension) Dependencies() []string {
	return []string{fabric.ExtensionName, provenance.ExtensionName}
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
const ErrResolvedTooEarly = "networkmodel: the engine is not built yet; something " +
	"resolved it before the " + ExtensionName + " extension started, which means " +
	"its Dependencies() does not name " + ExtensionName

// Start builds the engine.
func (e *Extension) Start(ctx context.Context) error {
	app := e.App()
	if app == nil {
		return errors.New("networkmodel: started without an application")
	}

	if e.store == nil {
		fab, err := forge.Inject[*fabriq.Fabriq](app.Container())
		if err != nil {
			return fmt.Errorf(
				"networkmodel: resolving the data fabric: %w; the fabric extension "+
					"must start first, which its Dependencies() declares", err)
		}

		fabricExt, err := forge.Inject[*fabric.Extension](app.Container())
		if err != nil {
			return fmt.Errorf("networkmodel: resolving the fabric extension: %w", err)
		}

		// The tenant comes from the fabric extension rather than this one's own
		// configuration. Two places naming the tenant is how a write ends up
		// scoped differently from the read that looks for it.
		built, err := fabriqstore.New(fab, fabricExt.Tenant())
		if err != nil {
			return err
		}

		e.store = built
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

	if e.store == nil {
		return nil
	}

	return e.store.Close()
}

// Health reports whether the store is reachable.
func (e *Extension) Health(ctx context.Context) error {
	if e.store == nil {
		return errors.New("networkmodel: no store; the extension has not started")
	}

	return e.store.Ping(ctx)
}

// Engine returns the built engine, or nil before Start.
func (e *Extension) Engine() *Engine { return e.engine }
