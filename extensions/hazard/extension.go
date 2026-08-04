package hazard

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/fabriq"

	"github.com/xraph/forge"
	"github.com/xraph/vessel"

	"github.com/xraph/blackstart/extensions/fabric"
	"github.com/xraph/blackstart/extensions/geo"
	"github.com/xraph/blackstart/extensions/hazard/store"
	fabriqstore "github.com/xraph/blackstart/extensions/hazard/store/fabriq"
	"github.com/xraph/blackstart/extensions/provenance"
	"github.com/xraph/blackstart/extensions/weather"
	"github.com/xraph/blackstart/pkg/apiauth"
	"github.com/xraph/blackstart/pkg/hooks"
)

// Extension hosts the hazard models in the Forge application.
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
			"Turns weather into conductor loading and dynamic line ratings",
		),
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Dependencies declares what must start first.
//
// Only the fabric. Weather, geometry and the recorder are resolved optionally
// at Start, matching how weather assembles its own halves: a deployment
// without them serves reads, and naming them here would make a read-only
// deployment refuse to boot.
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
const ErrResolvedTooEarly = "hazard: the engine is not built yet; something " +
	"resolved it before the " + ExtensionName + " extension started, which means " +
	"its Dependencies() does not name " + ExtensionName

// Start builds the engine.
func (e *Extension) Start(context.Context) error {
	app := e.App()
	if app == nil {
		return errors.New("hazard: started without an application")
	}

	if e.store == nil {
		fab, err := forge.Inject[*fabriq.Fabriq](app.Container())
		if err != nil {
			return fmt.Errorf(
				"hazard: resolving the data fabric: %w; the fabric extension "+
					"must start first, which its Dependencies() declares", err)
		}

		fabricExt, err := forge.Inject[*fabric.Extension](app.Container())
		if err != nil {
			return fmt.Errorf("hazard: resolving the fabric extension: %w", err)
		}

		built, err := fabriqstore.New(fab, fabricExt.Tenant())
		if err != nil {
			return err
		}

		e.store = built
	}

	bus, _ := forge.Inject[*hooks.Bus](app.Container())

	engine, err := NewEngine(e.store, bus, e.computeHalf(app)...)
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
		return fmt.Errorf("hazard: registering the HTTP controller: %w", err)
	}

	return nil
}

// computeHalf assembles the compute half if this deployment can support it.
//
// Weather and geometry together or not at all: a pass needs both to get from a
// span to the numbers over it, and half of the pair would fail on the first
// span rather than at startup. The recorder is separately optional, because a
// pass without one still computes and records the absence.
func (e *Extension) computeHalf(app forge.App) []EngineOption {
	weatherEngine, weatherErr := forge.Inject[*weather.Engine](app.Container())
	geoEngine, geoErr := forge.Inject[*geo.Engine](app.Container())

	if weatherErr != nil || geoErr != nil {
		return nil
	}

	recorder, _ := forge.Inject[provenance.Recorder](app.Container())

	return []EngineOption{WithCompute(weatherEngine, geoEngine, recorder)}
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
		return errors.New("hazard: no store; the extension has not started")
	}

	return e.store.Ping(ctx)
}

// Engine returns the built engine, or nil before Start.
func (e *Extension) Engine() *Engine { return e.engine }
