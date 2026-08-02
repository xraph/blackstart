package fabric

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/xraph/fabriq"
	"github.com/xraph/fabriq/core/registry"
	"github.com/xraph/fabriq/core/tenant"
	"github.com/xraph/forge"
	"github.com/xraph/grove"
	"github.com/xraph/vessel"

	"github.com/xraph/blackstart/internal/entities"
)

// ExtensionName is how this extension is identified to Forge and how other
// extensions declare a dependency on it.
const ExtensionName = "fabric"

// Extension hosts Fabriq in the Forge application.
type Extension struct {
	*forge.BaseExtension

	reg    *registry.Registry
	config Config

	mu     sync.Mutex
	fab    *fabriq.Fabriq
	stores *fabriq.Stores
}

var (
	_ forge.Extension = (*Extension)(nil)
	_ interface {
		Dependencies() []string
	} = (*Extension)(nil)
)

// New builds the extension over an already-assembled registry.
//
// The registry is a constructor argument rather than something gathered during
// Register, because Fabriq validates it inside Open and Forge's default
// lifecycle interleaves Register and Start per extension. See
// internal/entities.
func New(reg *registry.Registry, opts ...Option) *Extension {
	cfg := DefaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	return &Extension{
		BaseExtension: forge.NewBaseExtension(
			ExtensionName, "0.1.0",
			"Hosts the Fabriq data fabric: the single write path for every entity",
		),
		reg:    reg,
		config: cfg,
	}
}

// Dependencies declares that grove must start first, since this extension
// borrows its connection.
//
// Fabriq's own forgeext returns nil here, which leaves ordering to
// registration order — a silent trap if the two are ever registered the other
// way round. Naming the dependency lets Forge's topological sort enforce it.
func (e *Extension) Dependencies() []string { return []string{"grove"} }

// Register provides the Fabriq facade into the container.
//
// The constructor closes over the extension rather than over a value, because
// the facade does not exist until Start. Resolving before then is a wiring
// error and says so, rather than handing back a nil.
func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	if e.reg == nil {
		return errors.New("fabric: no entity registry; build one with internal/entities")
	}

	if err := e.assertGraphConfigured(); err != nil {
		return err
	}

	return vessel.Provide(app.Container(), func() (*fabriq.Fabriq, error) {
		e.mu.Lock()
		defer e.mu.Unlock()

		if e.fab == nil {
			return nil, errors.New(
				"fabric: the Fabriq facade is not open yet; an extension resolved " +
					"it before the fabric extension started, which means its " +
					"Dependencies() does not name " + ExtensionName)
		}

		return e.fab, nil
	}, vessel.WithAliases(ExtensionName))
}

// Start opens Fabriq.
func (e *Extension) Start(ctx context.Context) error {
	cfg := e.config.Fabriq

	if cfg.Postgres.DSN == "" && len(cfg.Shards) == 0 {
		gdb, err := e.resolveGrove()
		if err != nil {
			return err
		}

		cfg = cfg.WithInjectedGrove(gdb)

		e.Logger().Info("fabric: borrowing the host's database handle")
	}

	if e.config.RunWorker && cfg.Redis.Addr == "" {
		return errors.New(
			"fabric: the outbox worker needs a Redis address; without a worker " +
				"draining it, fabriq_outbox grows without bound and no projection " +
				"advances")
	}

	opts := make([]fabriq.Option, 0, len(e.config.Options)+1)
	if len(e.config.LifecycleHooks) > 0 {
		opts = append(opts, fabriq.WithLifecycleHook(e.config.LifecycleHooks...))
	}

	opts = append(opts, e.config.Options...)

	fab, stores, err := fabriq.Open(ctx, e.reg, cfg, opts...)
	if err != nil {
		return fmt.Errorf("fabric: opening fabriq: %w", err)
	}

	e.mu.Lock()
	e.fab, e.stores = fab, stores
	e.mu.Unlock()

	e.Logger().Info("fabric: opened",
		forge.F("entities", len(e.reg.All())),
		forge.F("graph", len(entities.GraphEntities(e.reg))),
		forge.F("search", len(entities.SearchEntities(e.reg))),
		forge.F("hooks", len(e.config.LifecycleHooks)),
	)

	e.MarkStarted()

	return nil
}

// Stop closes the facade. The borrowed grove handle is not closed — the host
// owns it, and closing another component's connection on the way down is a
// good way to break every other extension's shutdown.
func (e *Extension) Stop(context.Context) error {
	e.mu.Lock()
	fab := e.fab
	e.fab, e.stores = nil, nil
	e.mu.Unlock()

	e.MarkStopped()

	if fab == nil {
		return nil
	}

	if err := fab.Close(); err != nil {
		return fmt.Errorf("fabric: closing fabriq: %w", err)
	}

	return nil
}

// Health reports whether the source of truth is reachable.
func (e *Extension) Health(ctx context.Context) error {
	fab := e.Fabriq()
	if fab == nil {
		return errors.New("fabric: not started")
	}

	var probe []struct {
		OK int `grove:"ok"`
	}

	if err := fab.Relational().Query(e.scoped(ctx), &probe, "SELECT 1 AS ok"); err != nil {
		return fmt.Errorf("fabric: health probe: %w", err)
	}

	return nil
}

// Fabriq returns the facade, or nil before Start.
func (e *Extension) Fabriq() *fabriq.Fabriq {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.fab
}

// Stores returns Fabriq's store handles, or nil before Start.
func (e *Extension) Stores() *fabriq.Stores {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.stores
}

// Tenant returns the tenant identifier every call is scoped to.
func (e *Extension) Tenant() string { return e.config.Tenant }

// scoped stamps the tenant, which every Fabriq entry point requires.
func (e *Extension) scoped(ctx context.Context) context.Context {
	return tenant.MustWithTenant(ctx, e.config.Tenant)
}

func (e *Extension) resolveGrove() (*grove.DB, error) {
	c := e.App().Container()
	if c == nil {
		return nil, errors.New("fabric: the application has no container")
	}

	if name := e.config.GroveDatabase; name != "" {
		gdb, err := vessel.InjectNamed[*grove.DB](c, name)
		if err != nil {
			return nil, fmt.Errorf("fabric: resolving the %q database: %w", name, err)
		}

		return gdb, nil
	}

	gdb, err := vessel.Inject[*grove.DB](c)
	if err != nil {
		return nil, fmt.Errorf(
			"fabric: no database handle in the container and no DSN configured; "+
				"register the grove extension before this one: %w", err)
	}

	return gdb, nil
}
