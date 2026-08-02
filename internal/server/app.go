// Package server is the composition root shared by both binaries.
//
// blackstart (the server) and bsctl (the CLI) are the same application with
// different entry points. Wiring them separately would mean two places to
// forget an extension, and a CLI that quietly disagrees with the server about
// what the system contains — which for a tool whose job is to import and
// inspect a network model is the worst possible divergence.
//
// The Forge layout is cmd/<binary>/main.go for the entry point and
// apps/<name>/ for that binary's assembly, with anything shared between them
// here. Both apps/ packages are thin because everything real lives in this
// one, which is the arrangement that keeps the two from drifting.
package server

import (
	"errors"
	"fmt"

	chronicleext "github.com/xraph/chronicle/extension"
	"github.com/xraph/fabriq"
	"github.com/xraph/fabriq/core/registry"
	fabriqmigrations "github.com/xraph/fabriq/migrations"
	"github.com/xraph/forge"
	_ "github.com/xraph/grove/drivers/pgdriver"           // registers the "postgres" driver
	_ "github.com/xraph/grove/drivers/pgdriver/pgmigrate" // registers its migration executor
	groveext "github.com/xraph/grove/extension"

	"github.com/xraph/blackstart/extensions/contingency"
	"github.com/xraph/blackstart/extensions/datasource"
	dsmigrations "github.com/xraph/blackstart/extensions/datasource/store/fabriq"
	"github.com/xraph/blackstart/extensions/fabric"
	"github.com/xraph/blackstart/extensions/networkmodel"
	nmmigrations "github.com/xraph/blackstart/extensions/networkmodel/store/fabriq"
	"github.com/xraph/blackstart/extensions/powerflow"
	"github.com/xraph/blackstart/extensions/provenance"
	"github.com/xraph/blackstart/extensions/topology"
	"github.com/xraph/blackstart/internal/entities"
	"github.com/xraph/blackstart/pkg/buildinfo"
)

// Config is everything the host needs to start.
type Config struct {
	// Name distinguishes the two binaries in logs and health output.
	Name string

	// HTTPAddress is where the server listens. Empty means no listener, which
	// is what bsctl wants — it runs commands and exits.
	HTTPAddress string

	// Environment is "development", "staging" or "production".
	Environment string

	// DatabaseDSN is the PostgreSQL connection string.
	DatabaseDSN string

	// RedisAddr is required whenever the outbox worker runs.
	RedisAddr string

	// FalkorDBAddr enables the graph projection. Required whenever any entity
	// declares one, which the fabric extension asserts.
	FalkorDBAddr string

	// Tenant is the single tenant every call is scoped to.
	Tenant string

	// SolverVersion is recorded with every contingency run.
	//
	// The screen stream does not carry one — the contract puts version fields
	// on the solve response, since repeating them across thousands of streamed
	// results would be noise — so the host supplies it. Left empty, the
	// extension records a conspicuous placeholder rather than a plausible
	// version, so the gap is visible in the record.
	SolverVersion string

	// SolverAddress is where the Rust power-flow service listens.
	//
	// Empty means no solver, which is a legitimate configuration: `bsctl
	// entities` and `blackstart migrate` have no use for one, and requiring it
	// would make every command depend on a process it never calls. The
	// extensions that need it fail at Start with a message naming this
	// setting, rather than at first use.
	SolverAddress string

	// RunWorker starts the outbox relay in this process. The server does; the
	// CLI does not, because a short-lived command should not take over draining
	// a shared queue and then exit mid-batch.
	RunWorker bool
}

// DefaultConfig returns a configuration suitable for local development.
func DefaultConfig(name string) Config {
	return Config{
		Name:        name,
		Environment: "development",
		Tenant:      fabric.DefaultTenant,
	}
}

// Validate reports whether the configuration can start.
func (c Config) Validate() error {
	if c.DatabaseDSN == "" {
		return errors.New("app: a database DSN is required")
	}

	if c.RunWorker && c.RedisAddr == "" {
		return errors.New(
			"app: the outbox worker needs a Redis address; without a worker " +
				"draining it, fabriq_outbox grows without bound")
	}

	return nil
}

// Build assembles the application.
//
// The order here is load-bearing and is the reason this function exists rather
// than each binary wiring its own: the entity registry must be complete before
// the fabric extension is constructed, because Fabriq validates it inside Open
// and Forge's default lifecycle interleaves Register and Start per extension.
// See internal/entities.
func Build(cfg Config, contributors ...entities.Contributor) (forge.App, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	reg, err := entities.Build(contributors...)
	if err != nil {
		return nil, err
	}

	groveExt := groveext.New(
		groveext.WithDSN("postgres", cfg.DatabaseDSN),
		// Not groveext.WithDisableMigrate(): that flag refuses the explicit
		// Migrate call too, so `blackstart migrate up` fails with "migrations
		// are disabled via configuration" — which reads as a configuration
		// mistake and is actually the code refusing its own command.
		//
		// forge.WithDisableMigrations() below is the right knob. It stops the
		// automatic run at startup, which is the thing worth preventing: a
		// migration racing N replicas coming up is a bad way to learn why
		// Fabriq's documentation insists migrations are a discrete deploy step.
		// Every extension's migration group is handed to one orchestrator, in
		// dependency order. Grove resolves that ordering topologically, but only
		// within a single orchestrator — registering each extension's group
		// separately makes DependsOn a silent no-op, which its own source notes.
		groveext.WithMigrations(
			fabriqmigrations.Group(),
			dsmigrations.Group(),
			nmmigrations.Group(),
		),
	)

	fabricExt := fabric.New(reg, fabric.WithConfig(fabricConfig(cfg)))

	// Chronicle is the audit trail. Blackstart does not own one: chronicle is
	// hash-chained, supports per-subject crypto-erasure, and produces
	// compliance reports, none of which a hand-written append-only table would
	// have. See a private design note.
	//
	// It borrows the same grove database, so its migrations run alongside the
	// platform's rather than needing a second connection to manage.
	auditExt := chronicleext.New(
		chronicleext.WithGroveDatabase(""),
		chronicleext.WithCryptoErasure(true),
	)

	extensions := []forge.Extension{
		groveExt, fabricExt, auditExt,
		provenance.New(), networkmodel.New(), datasource.New(), topology.New(),
	}

	// The solver-backed extensions are registered only when there is a solver
	// to reach. Registering them without one would mean every startup logs a
	// failure for a capability the operator did not ask for.
	if cfg.SolverAddress != "" {
		extensions = append(extensions,
			powerflow.New(),
			contingency.New(contingency.WithSolverVersion(cfg.SolverVersion)),
		)
	}

	// Forge runs its own discovery for extension configuration, and its default
	// search path is the working directory only — it never descends into
	// configs/. Without these paths, everything under `extensions:` in
	// configs/config.yaml is inert: no error, no warning, the block simply
	// never reaches any extension. Pointing Forge at the same directories this
	// package reads keeps one file authoritative for both.
	searchPaths, err := ConfigSearchPaths()
	if err != nil {
		return nil, err
	}

	app := forge.New(
		forge.WithConfigSearchPaths(searchPaths...),
		forge.WithAppName(cfg.Name),
		forge.WithAppVersion(buildinfo.Get().Version),
		forge.WithAppDescription("Weather-driven outage risk and cascade analysis"),
		forge.WithAppEnvironment(cfg.Environment),
		forge.WithHTTPAddress(cfg.HTTPAddress),
		// Migrations are run by `bsctl migrate`, not at startup.
		forge.WithDisableMigrations(),
	)

	for _, ext := range extensions {
		if err := app.RegisterExtension(ext); err != nil {
			return nil, fmt.Errorf("app: registering %s: %w", ext.Name(), err)
		}
	}

	// The solver client is provided before the extensions start, since both
	// solver-backed extensions resolve it in their own Start.
	if err := provideSolver(app, cfg.SolverAddress); err != nil {
		return nil, err
	}

	// Forge builds a HealthManager and runs its checks but wires no route for
	// it on the application router, so without this the platform reports its
	// health to nobody.
	if err := app.RegisterController(&healthController{app: app}); err != nil {
		return nil, fmt.Errorf("app: registering the health controller: %w", err)
	}

	return app, nil
}

func fabricConfig(cfg Config) fabric.Config {
	fc := fabric.DefaultConfig()
	fc.Tenant = cfg.Tenant
	fc.RunWorker = cfg.RunWorker
	fc.Fabriq.Redis.Addr = cfg.RedisAddr
	fc.Fabriq.FalkorDB.Addr = cfg.FalkorDBAddr

	// Graph and search projections only advance when a worker drains the
	// outbox, so declaring them without one would be a lie about what the
	// system maintains.
	fc.Fabriq.Projections.Graph = cfg.FalkorDBAddr != "" && cfg.RunWorker

	return fc
}

// Fabric resolves the Fabriq facade from a built application.
//
// Callers get an error rather than a nil when the application has not started,
// because a nil facade fails on first use somewhere far from the cause.
func Fabric(app forge.App) (*fabriq.Fabriq, error) {
	fab, err := forge.Inject[*fabriq.Fabriq](app.Container())
	if err != nil {
		return nil, fmt.Errorf("app: resolving the data fabric: %w", err)
	}

	return fab, nil
}

// Registry is exported for commands that need to inspect what is registered
// without opening a connection — `bsctl entities`, and the startup log line.
func Registry(contributors ...entities.Contributor) (*registry.Registry, error) {
	return entities.Build(contributors...)
}
