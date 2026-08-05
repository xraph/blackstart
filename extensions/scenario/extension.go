package scenario

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/xraph/fabriq"

	"github.com/xraph/forge"
	"github.com/xraph/vessel"

	"github.com/xraph/blackstart/extensions/cascade"
	"github.com/xraph/blackstart/extensions/contingency"
	"github.com/xraph/blackstart/extensions/fabric"
	"github.com/xraph/blackstart/extensions/geo"
	"github.com/xraph/blackstart/extensions/hazard"
	hzfabriq "github.com/xraph/blackstart/extensions/hazard/store/fabriq"
	"github.com/xraph/blackstart/extensions/networkmodel"
	scnstore "github.com/xraph/blackstart/extensions/scenario/store"
	fabriqstore "github.com/xraph/blackstart/extensions/scenario/store/fabriq"
	"github.com/xraph/blackstart/extensions/weather"
	"github.com/xraph/blackstart/pkg/apiauth"
	"github.com/xraph/blackstart/pkg/hooks"
)

// ExtensionName is how this extension is identified to Forge and how other
// extensions declare a dependency on it.
const ExtensionName = "scenario"

// Extension hosts the scenario engine and its run queue in the Forge
// application.
type Extension struct {
	*forge.BaseExtension

	store  scnstore.Store
	engine *Engine

	// runWorker drains the queue in this process. The server does; bsctl does
	// not, for the reason it does not drain the outbox either — a short-lived
	// command should not claim a run it will abandon partway through.
	runWorker bool

	// Worker lifecycle, guarded by the same mutex. Used only by runner.go,
	// declared here because a struct's fields cannot live in two files.
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// Option configures the extension.
type Option func(*Extension)

// WithStore supplies a store, overriding the Fabriq-backed default.
//
// Fabriq-backed by default rather than in-memory, and for a stronger reason
// than cascade has: a scenario is a definition someone authored, not derived
// data. A cascade run can at least be re-simulated from the model it names; a
// scenario that vanished on restart is a binding nobody can reconstruct,
// because the thing that knew it was the person who typed it.
func WithStore(s scnstore.Store) Option {
	return func(e *Extension) { e.store = s }
}

// WithWorker starts the queue drainer in this process.
//
// One flag and no interval. DefaultWorkerInterval is not configuration: the
// queue is durable state that a tick either finds work in or does not, so the
// cost of asking often is one Claim query against an index built for it —
// there is no remote call to pace and nothing for an operator to tune away
// from.
func WithWorker(enabled bool) Option {
	return func(e *Extension) { e.runWorker = enabled }
}

// New builds the extension.
func New(opts ...Option) *Extension {
	e := &Extension{
		BaseExtension: forge.NewBaseExtension(
			ExtensionName, "0.1.0",
			"Binds a model, a forecast and a policy into reproducible runs and executes them",
		),
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Dependencies declares what must start first.
//
// Longer than its siblings' lists, and every name earns its place: Start
// resolves a built engine out of the container for each of networkmodel,
// weather, geo, contingency and cascade, and an engine resolved before its own
// extension started answers with that extension's resolved-too-early error
// rather than with a nil. Ordering is what makes this extension's Start the
// last of the group rather than a coin toss over registration order.
//
// hazard is named for a data dependency rather than a container one: Start
// builds a reader over hazard's own value tables, and reading a schema whose
// owner has not started is the kind of thing that works until the day it does
// not.
//
// A name the application never registered is skipped by Forge's graph rather
// than refused, which is what makes this list safe in a deployment without a
// solver — where contingency and cascade are absent, and so is this extension.
func (e *Extension) Dependencies() []string {
	return []string{
		fabric.ExtensionName,
		networkmodel.ExtensionName,
		weather.ExtensionName,
		geo.ExtensionName,
		hazard.ExtensionName,
		contingency.ExtensionName,
		cascade.ExtensionName,
	}
}

// Register provides the engine into the container.
//
// The constructor closes over the extension rather than over a value, because
// the engine does not exist until Start — it needs five other engines and a
// store. Resolving before then is a wiring error and says so rather than
// handing back a nil.
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
const ErrResolvedTooEarly = "scenario: the engine is not built yet; something " +
	"resolved it before the " + ExtensionName + " extension started, which means " +
	"its Dependencies() does not name " + ExtensionName

// Start builds the engine and registers the HTTP surface.
func (e *Extension) Start(context.Context) error {
	app := e.App()
	if app == nil {
		return errors.New("scenario: started without an application")
	}

	fab, err := forge.Inject[*fabriq.Fabriq](app.Container())
	if err != nil {
		return fmt.Errorf(
			"scenario: resolving the data fabric: %w; the fabric extension must "+
				"start first, which its Dependencies() declares", err)
	}

	fabricExt, err := forge.Inject[*fabric.Extension](app.Container())
	if err != nil {
		return fmt.Errorf("scenario: resolving the fabric extension: %w", err)
	}

	if e.store == nil {
		built, err := fabriqstore.New(fab, fabricExt.Tenant())
		if err != nil {
			return err
		}

		e.store = built
	}

	collaborators, err := e.collaborators(app, fab, fabricExt.Tenant())
	if err != nil {
		return err
	}

	// The bus is optional. A host that publishes no domain events is a
	// legitimate configuration — bsctl is one.
	bus, _ := forge.Inject[*hooks.Bus](app.Container())

	engine, err := NewEngine(e.store, collaborators, bus)
	if err != nil {
		return err
	}

	e.engine = engine

	// Registered here rather than in Register, because the controller needs
	// the engine and the engine needs five others. Forge accepts a controller
	// after startup; it would not accept a nil engine behind one.
	//
	// Optional guard: an unauthenticated deployment provides none, and a nil
	// guard declares no middleware. A configured one that cannot resolve its
	// middleware refuses every request rather than serving it open.
	guard, _ := forge.Inject[*apiauth.Guard](app.Container())

	if err := app.RegisterController(NewController(engine, guard)); err != nil {
		return fmt.Errorf("scenario: registering the HTTP controller: %w", err)
	}

	return nil
}

// collaborators assembles everything outside this extension the pipeline
// calls.
//
// Every one is required. Collaborators.Validate would refuse a partial set
// anyway, but failing here names the extension that is missing rather than
// listing the role nothing filled — and a run that reached a nil stage would
// fail every execution at the same place rather than at startup.
func (e *Extension) collaborators(
	app forge.App, fab *fabriq.Fabriq, tenant string,
) (Collaborators, error) {
	models, err := forge.Inject[*networkmodel.Engine](app.Container())
	if err != nil {
		return Collaborators{}, fmt.Errorf(
			"scenario: resolving the network-model engine: %w", err)
	}

	cycles, err := forge.Inject[*weather.Engine](app.Container())
	if err != nil {
		return Collaborators{}, fmt.Errorf("scenario: resolving the weather engine: %w", err)
	}

	geometry, err := forge.Inject[*geo.Engine](app.Container())
	if err != nil {
		return Collaborators{}, fmt.Errorf("scenario: resolving the geometry engine: %w", err)
	}

	screener, err := forge.Inject[*contingency.Engine](app.Container())
	if err != nil {
		return Collaborators{}, fmt.Errorf("scenario: resolving the contingency engine: %w", err)
	}

	simulator, err := forge.Inject[*cascade.Engine](app.Container())
	if err != nil {
		return Collaborators{}, fmt.Errorf("scenario: resolving the cascade engine: %w", err)
	}

	// hazard's STORE, not its engine, and that is not a shortcut around the
	// container. The rating step needs one batch read — the ampacity of every
	// span of a branch, at one instant, in one round trip — and *hazard.Engine
	// has no batch method. Widening it to grow one would change a package a
	// concurrent effort owns, to add a method whose only caller reads values
	// hazard has already computed and stored. Reading them where they were
	// stored is the smaller claim.
	hazardStore, err := hzfabriq.New(fab, tenant)
	if err != nil {
		return Collaborators{}, fmt.Errorf("scenario: building the hazard reader: %w", err)
	}

	ampacities, err := NewSpanAmpacities(hazardStore.Values())
	if err != nil {
		return Collaborators{}, err
	}

	return Collaborators{
		Models:    models,
		Cycles:    cycles,
		Geo:       geometry,
		Hazard:    ampacities,
		Screener:  screener,
		Simulator: simulator,
	}, nil
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
// The five collaborating engines are deliberately not probed. Each reports its
// own health, and reporting unhealthy here because one of them was would say
// this extension is broken when what is broken is somewhere a reader can
// already see.
func (e *Extension) Health(ctx context.Context) error {
	if e.store == nil {
		return errors.New("scenario: no store; the extension has not started")
	}

	return e.store.Ping(ctx)
}

// Engine returns the built engine, or nil before Start.
func (e *Extension) Engine() *Engine { return e.engine }
