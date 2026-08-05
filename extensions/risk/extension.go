package risk

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/fabriq"

	"github.com/xraph/forge"
	"github.com/xraph/vessel"

	"github.com/xraph/blackstart/extensions/fabric"
	"github.com/xraph/blackstart/extensions/geo"
	"github.com/xraph/blackstart/extensions/hazard"
	"github.com/xraph/blackstart/extensions/provenance"
	"github.com/xraph/blackstart/extensions/risk/store"
	fabriqstore "github.com/xraph/blackstart/extensions/risk/store/fabriq"
	"github.com/xraph/blackstart/extensions/weather"
	"github.com/xraph/blackstart/pkg/apiauth"
	"github.com/xraph/blackstart/pkg/hooks"
)

// Guarded at compile time. A reviewer matched these four method signatures
// against *hazard.Engine, *weather.Engine and *geo.Engine by hand; nothing
// short of the compiler catches the next drift, and until this extension was
// wired in, a mismatch would have surfaced at runtime — the first time
// something actually resolved these engines into an EngineOption — rather
// than here.
var (
	_ Hazard   = (*hazard.Engine)(nil)
	_ Weather  = (*weather.Engine)(nil)
	_ Geometry = (*geo.Engine)(nil)
)

// Extension hosts the risk models in the Forge application.
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
			"Turns conductor loading into per-component failure probabilities",
		),
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Dependencies declares what must start first.
//
// Only the fabric. Hazard, weather, geometry and the recorder are resolved
// optionally at Start, matching how hazard itself assembles its own compute
// half: a deployment without them serves reads, and naming them here would
// make a read-only deployment refuse to boot.
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
const ErrResolvedTooEarly = "risk: the engine is not built yet; something " +
	"resolved it before the " + ExtensionName + " extension started, which means " +
	"its Dependencies() does not name " + ExtensionName

// Start builds the engine.
func (e *Extension) Start(context.Context) error {
	app := e.App()
	if app == nil {
		return errors.New("risk: started without an application")
	}

	if e.store == nil {
		fab, err := forge.Inject[*fabriq.Fabriq](app.Container())
		if err != nil {
			return fmt.Errorf(
				"risk: resolving the data fabric: %w; the fabric extension "+
					"must start first, which its Dependencies() declares", err)
		}

		fabricExt, err := forge.Inject[*fabric.Extension](app.Container())
		if err != nil {
			return fmt.Errorf("risk: resolving the fabric extension: %w", err)
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
		return fmt.Errorf("risk: registering the HTTP controller: %w", err)
	}

	return nil
}

// computeHalf assembles the compute half if this deployment can support it.
//
// Hazard, weather and geometry together or not at all: a pass needs all three
// to get from a weather cycle to a probability, and starting with only some
// of them would fail on the first component rather than at startup. The
// recorder is separately optional, because a pass without one still computes
// and records the absence.
//
// This is why risk.New() must be registered after hazard.New(), weather.New()
// and geo.New() in the host's extension list (internal/server/app.go):
// Forge's default lifecycle interleaves Register and Start per extension, so
// an engine resolved here has to have already been built by its own
// extension's Start, not merely declared.
func (e *Extension) computeHalf(app forge.App) []EngineOption {
	hazardEngine, hazardErr := forge.Inject[*hazard.Engine](app.Container())
	weatherEngine, weatherErr := forge.Inject[*weather.Engine](app.Container())
	geoEngine, geoErr := forge.Inject[*geo.Engine](app.Container())

	if hazardErr != nil || weatherErr != nil || geoErr != nil {
		return nil
	}

	recorder, _ := forge.Inject[provenance.Recorder](app.Container())

	return []EngineOption{WithCompute(hazardEngine, weatherEngine, geoEngine, recorder)}
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
		return errors.New("risk: no store; the extension has not started")
	}

	return e.store.Ping(ctx)
}

// Engine returns the built engine, or nil before Start.
func (e *Extension) Engine() *Engine { return e.engine }
