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
	"time"

	authsomeext "github.com/xraph/authsome/extension"
	authsomemw "github.com/xraph/authsome/middleware"
	apikeyplugin "github.com/xraph/authsome/plugins/apikey"
	mfaplugin "github.com/xraph/authsome/plugins/mfa"
	passkeyplugin "github.com/xraph/authsome/plugins/passkey"
	passwordplugin "github.com/xraph/authsome/plugins/password"
	chronicleext "github.com/xraph/chronicle/extension"
	"github.com/xraph/fabriq"
	"github.com/xraph/fabriq/core/registry"
	fabriqmigrations "github.com/xraph/fabriq/migrations"
	"github.com/xraph/forge"
	"github.com/xraph/vessel"
	wardenext "github.com/xraph/warden/extension"

	"github.com/xraph/blackstart/pkg/apiauth"

	dashboardExt "github.com/xraph/forge/extensions/dashboard"
	_ "github.com/xraph/grove/drivers/pgdriver"           // registers the "postgres" driver
	_ "github.com/xraph/grove/drivers/pgdriver/pgmigrate" // registers its migration executor
	groveext "github.com/xraph/grove/extension"

	"github.com/xraph/blackstart/extensions/cascade"
	cscmigrations "github.com/xraph/blackstart/extensions/cascade/store/fabriq"
	"github.com/xraph/blackstart/extensions/contingency"
	ctgmigrations "github.com/xraph/blackstart/extensions/contingency/store/fabriq"
	"github.com/xraph/blackstart/extensions/datasource"
	dsmigrations "github.com/xraph/blackstart/extensions/datasource/store/fabriq"
	"github.com/xraph/blackstart/extensions/fabric"
	"github.com/xraph/blackstart/extensions/geo"
	geomigrations "github.com/xraph/blackstart/extensions/geo/store/fabriq"
	"github.com/xraph/blackstart/extensions/hazard"
	hzmigrations "github.com/xraph/blackstart/extensions/hazard/store/fabriq"
	"github.com/xraph/blackstart/extensions/networkmodel"
	nmmigrations "github.com/xraph/blackstart/extensions/networkmodel/store/fabriq"
	"github.com/xraph/blackstart/extensions/powerflow"
	pfmigrations "github.com/xraph/blackstart/extensions/powerflow/store/fabriq"
	"github.com/xraph/blackstart/extensions/provenance"
	pvmigrations "github.com/xraph/blackstart/extensions/provenance/store/fabriq"
	"github.com/xraph/blackstart/extensions/scenario"
	scnmigrations "github.com/xraph/blackstart/extensions/scenario/store/fabriq"
	"github.com/xraph/blackstart/extensions/scl"
	sclmigrations "github.com/xraph/blackstart/extensions/scl/store/fabriq"
	"github.com/xraph/blackstart/extensions/topology"
	"github.com/xraph/blackstart/extensions/weather"
	"github.com/xraph/blackstart/extensions/weather/model"
	wxmigrations "github.com/xraph/blackstart/extensions/weather/store/fabriq"
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

	// BlobDir is where the content-addressed byte plane keeps its objects.
	//
	// A filesystem path in development; a driver URL in a deployment that has
	// object storage. Empty disables the byte plane, and an import then fails
	// rather than silently keeping megabytes of canonical encoding in a
	// Postgres column — which is where they used to live, and is what this
	// replaced.
	BlobDir string

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

	// WeatherSource and WeatherFeeds name what the ingest loop retrieves: the
	// registry source slug, and the feed slug within it per model.
	//
	// There is no separate enable flag. The loop runs when both of these are
	// set and RunWorker is true, because a feed configured in a process that
	// drains no workers is inert — the same reasoning that ties the graph
	// projection to the outbox relay. Leaving them empty is how a deployment
	// says it does not ingest weather.
	WeatherSource string
	WeatherFeeds  map[string]string

	// WeatherInterval, WeatherBackfill and WeatherHours tune the loop. Zero
	// means the extension's own default.
	WeatherInterval time.Duration
	WeatherBackfill time.Duration
	WeatherHours    []int

	// HazardInterval and HazardLookback tune the rating loop, which runs
	// whenever RunWorker is true. It needs no registry slugs: it reads what
	// weather already stored.
	HazardInterval time.Duration
	HazardLookback time.Duration

	// EnableAuth registers Authsome as this application's own authentication
	// authority.
	//
	// In-process and single-tenant, not a client of an identity service. There
	// is one blackstart, one database and one operator population, so the
	// client-mode arrangement — where every binary delegates to a central
	// authority over HTTP — would add a network hop and a second deployment to
	// answer a question this process can answer locally.
	//
	// Off for the CLI. Authsome protects the HTTP surface, and `bsctl` serves
	// none: registering it there would make every command pay for a subsystem
	// it never reaches.
	EnableAuth bool
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
			pvmigrations.Group(),
			nmmigrations.Group(),
			geomigrations.Group(),
			sclmigrations.Group(),
			pfmigrations.Group(),
			ctgmigrations.Group(),
			wxmigrations.Group(),
			hzmigrations.Group(),
			cscmigrations.Group(),
			scnmigrations.Group(),
		),
	)

	fabricExt := fabric.New(reg, fabric.WithConfig(fabricConfig(cfg)))

	// Chronicle is the audit trail. Blackstart does not own one: chronicle is
	// hash-chained, supports per-subject crypto-erasure, and produces
	// compliance reports, none of which a hand-written append-only table would
	// have.
	//
	// It borrows the same grove database, so its migrations run alongside the
	// platform's rather than needing a second connection to manage.
	auditExt := chronicleext.New(
		chronicleext.WithGroveDatabase(""),
		chronicleext.WithCryptoErasure(true),
		chronicleext.WithDisableRoutes(),
	)

	extensions := []forge.Extension{
		groveExt, fabricExt, auditExt,
		provenance.New(), networkmodel.New(), datasource.New(),
		topology.New(), geo.New(), scl.New(),
		weather.New(weather.WithPolling(weatherPollConfig(cfg))),
		hazard.New(hazard.WithComputeSchedule(hazardComputeConfig(cfg))),
	}

	// Authsome, this application's own authentication authority.
	//
	// The plugin set is deliberately short. Organization, SCIM, SSO, the
	// OAuth2 provider, social login and subscription are all multi-tenancy and
	// SaaS concerns — blackstart has one tenant, one operator population and
	// nothing to bill — and registering them would create tables, routes and a
	// configuration surface for capabilities nobody here can use.
	//
	// What remains is what a single-tenant operator console actually needs: a
	// password credential, API keys for service-to-service and scripted
	// access, and two factors — TOTP and passkeys — for the console itself.
	var authority *authsomeext.Extension

	if cfg.EnableAuth {
		// Warden first, and that ordering is enforced rather than conventional:
		// Authsome resolves *warden.Engine from the container during its own
		// Register and refuses to start without it. Warden is the authorization
		// half — roles and permissions — to Authsome's authentication half.
		extensions = append(extensions, wardenext.New(
			wardenext.WithGroveDatabase(""),
			wardenext.WithDisableRoutes(),
		))

		authExt := authsomeext.New(
			// Mounted outside /api/v1 on purpose. Authsome contributes ~84
			// paths, and under the domain prefix they outnumber the 25 that
			// model the grid by more than three to one — so "generate a client
			// for /api/v1" would produce a client that is mostly identity.
			//
			// A separate root makes the split a prefix rule rather than a
			// hand-maintained exclusion list, and it says something true: the
			// grid API and the identity API are different surfaces with
			// different audiences and different lifecycles.
			authsomeext.WithBasePath("/identity"),
			// The same grove database as everything else. A second connection
			// to manage would be a second thing to get wrong in a deployment
			// whose whole point is one binary and one database.
			authsomeext.WithGroveDatabase(""),
			authsomeext.WithPlugins(
				passwordplugin.New(),
				apikeyplugin.New(),
				mfaplugin.New(),
				passkeyplugin.New(),
			),
		)

		extensions = append(extensions, authExt)

		// Captured rather than resolved from the container. Authsome does not
		// provide itself there — `no service registered for type
		// *extension.Extension` is what the guard reported until this changed —
		// and it is constructed right here, so closing over the instance is
		// both simpler and one fewer thing that can be absent at runtime.
		authority = authExt
	}

	// The operator dashboard, last in the list on purpose: it calls
	// DashboardContributor on every other extension during its own Start, so
	// anything registered after it would be invisible to it.
	//
	// Standalone rather than a discovery contributor. Blackstart is one binary
	// with one database and no aggregating portal in front of it, so the
	// discovery machinery would poll for peers that do not exist. It becomes a
	// contributor by configuration if that ever changes.
	extensions = append(extensions, dashboardExt.NewExtension(
		dashboardExt.WithTitle(cfg.Name+" — operations"),
		dashboardExt.WithBasePath("/_forge/dashboard"),
		dashboardExt.WithDiscovery(false),
		dashboardExt.WithRealtime(true),
		dashboardExt.WithSearch(true),
		dashboardExt.WithRefreshInterval(10*time.Second),
	))

	// The solver-backed extensions are registered only when there is a solver
	// to reach. Registering them without one would mean every startup logs a
	// failure for a capability the operator did not ask for.
	if cfg.SolverAddress != "" {
		extensions = append(extensions,
			powerflow.New(),
			contingency.New(contingency.WithSolverVersion(cfg.SolverVersion)),
			cascade.New(),
			// Last of the group, and its Dependencies() names the other four
			// rather than relying on that: it resolves a built engine from
			// each of them at Start.
			//
			// Registered only where there is a solver, like its two
			// collaborators — a scenario orchestrates a sweep and a cascade,
			// so an extension serving one without them would accept
			// definitions and fail every run.
			scenario.New(scenario.WithWorker(cfg.RunWorker)),
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
		// Generated from the same schema declarations the router enforces, so
		// the specification cannot drift from the API it documents. Served at
		// /docs with the raw document at /openapi.json.
		forge.WithAppRouterOptions(routerOptions(cfg)...),
		// Migrations are run by `bsctl migrate`, not at startup.
		forge.WithDisableMigrations(),
	)

	// The guard every API controller declares. Provided before the extensions
	// register so each can resolve it in Start, and resolved lazily inside so
	// this does not depend on Authsome having finished starting.
	//
	// Fail-closed: when an authority is configured and its middleware cannot
	// be resolved, every guarded request is refused rather than served
	// unauthenticated. A guard that exists and does nothing is worse than none
	// at all, because the API looks protected.
	if cfg.EnableAuth {
		guard := apiauth.New(func() (forge.Middleware, error) {
			// Captured rather than resolved from the container. Authsome does
			// not provide itself there — "no service registered for type
			// *extension.Extension" is what the guard reported until this
			// changed — and it is constructed in this same function, so
			// closing over it is both simpler and one fewer thing that can be
			// absent at runtime.
			if authority == nil {
				return nil, errors.New("app: no authentication authority was built")
			}

			// Two middlewares, and both are needed. AuthMiddleware resolves a
			// credential into context and lets an anonymous request through —
			// it is the identity producer, not the gate. RequireAuth is the
			// gate. Declaring only the first is how an API ends up looking
			// protected and answering 200 to everyone, which is what happened
			// on the first attempt here.
			return chain(authority.AuthMiddleware(), authsomemw.RequireAuth()), nil
		})

		if err := vessel.ProvideValue(app.Container(), guard); err != nil {
			return nil, fmt.Errorf("app: providing the API guard: %w", err)
		}
	}

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

// weatherPollConfig assembles the ingest loop's settings.
//
// Enabled is derived rather than configured: a loop needs a registry source
// and at least one feed to have anything to ask for, and it needs a process
// that runs workers. Declaring it on without either would be the same lie as
// declaring a graph projection with no relay to advance it.
func weatherPollConfig(cfg Config) weather.PollConfig {
	pc := weather.DefaultPollConfig()

	pc.SourceSlug = cfg.WeatherSource
	pc.Feeds = weatherFeeds(cfg.WeatherFeeds)

	if cfg.WeatherInterval > 0 {
		pc.Interval = cfg.WeatherInterval
	}

	if cfg.WeatherBackfill > 0 {
		pc.Backfill = cfg.WeatherBackfill
	}

	if len(cfg.WeatherHours) > 0 {
		pc.Hours = cfg.WeatherHours
	}

	pc.Enabled = cfg.RunWorker && pc.SourceSlug != "" && len(pc.Feeds) > 0

	return pc
}

// weatherFeeds converts the configured feed map into the model vocabulary.
//
// A key that names no model is dropped rather than failing the load. The
// vocabulary is closed and the file is hand-edited, so a typo should cost that
// one model's ingest and not the whole process — and the startup log reports
// which models are actually polled.
func weatherFeeds(configured map[string]string) map[model.Model]string {
	if len(configured) == 0 {
		return nil
	}

	out := make(map[model.Model]string, len(configured))

	for _, known := range model.All() {
		if slug, ok := configured[string(known)]; ok && slug != "" {
			out[known] = slug
		}
	}

	return out
}

// hazardComputeConfig assembles the rating loop's settings.
//
// No registry slugs to check: this loop reads what weather already stored, so
// running a worker is the whole condition.
func hazardComputeConfig(cfg Config) hazard.ComputeConfig {
	hc := hazard.DefaultComputeConfig()

	if cfg.HazardInterval > 0 {
		hc.Interval = cfg.HazardInterval
	}

	if cfg.HazardLookback > 0 {
		hc.Lookback = cfg.HazardLookback
	}

	hc.Enabled = cfg.RunWorker

	return hc
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

	// The content-addressable layer, not just the byte plane. Canonical
	// encodings are keyed by their own content hash everywhere else in the
	// system, so storing them under anything else would mean maintaining a
	// second identity for the same bytes.
	if cfg.BlobDir != "" {
		fc.Fabriq.Storage.StorageDriver = cfg.BlobDir
		fc.Fabriq.Storage.DefaultBucket = "blackstart"
		fc.Fabriq.Storage.EnableCas = true
	}

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

// chain composes middlewares so the first wraps the rest.
//
// Order is load-bearing: the identity producer has to run before the gate, or
// the gate finds nothing in context and refuses every request including the
// authenticated ones.
func chain(mws ...forge.Middleware) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		for index := len(mws) - 1; index >= 0; index-- {
			next = mws[index](next)
		}

		return next
	}
}
