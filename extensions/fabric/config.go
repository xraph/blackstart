// Package fabric hosts Fabriq inside the Forge application.
//
// It exists instead of Fabriq's own forgeext because that extension calls
// fabriq.Open with no variadic options:
//
//	// fabriq/forgeext/extension.go
//	fab, stores, err := fabriq.Open(ctx, e.reg, cfg)
//
// Every code-level seam Fabriq offers is a fabriq.Option, so routing through
// forgeext makes all of them unreachable — WithLifecycleHook, WithUpcasters,
// WithAuthz, WithClock. Two of those are load-bearing here:
//
//   - WithLifecycleHook runs inside the write transaction. It is how audit
//     becomes gapless (a failed audit rolls back the domain write) and how
//     PostGIS geometry becomes atomic with the entity it belongs to. Neither
//     is achievable any other way: projection.CustomApplier cannot express a
//     geometry write, because projection.Mutation is a sealed union whose only
//     sinks are the graph and search engines.
//
//   - WithUpcasters is how event payload shapes evolve. Without it, the first
//     schema change to a stored event is unrecoverable.
//
// See a private design note.
package fabric

import (
	"time"

	"github.com/xraph/fabriq"
	"github.com/xraph/fabriq/core/command"
)

// Config configures the extension.
type Config struct {
	// Fabriq is the datastore configuration. When its Postgres DSN is empty
	// the extension borrows a *grove.DB from the container instead, which is
	// the normal arrangement: the host owns the connection.
	Fabriq fabriq.Config

	// GroveDatabase names the grove handle to borrow when several are
	// registered. Empty resolves the unnamed one.
	GroveDatabase string

	// Tenant is the single tenant every call is scoped to. Fabriq has no
	// default-tenant mode by design — a missing tenant is a hard failure at
	// every entry point — so blackstart supplies a constant.
	Tenant string

	// RunWorker starts the outbox relay and projection consumers in this
	// process. Without a worker somewhere, fabriq_outbox grows without bound
	// and no projection ever advances, so a deployment with none is a
	// misconfiguration rather than a lighter-weight option.
	RunWorker bool

	// ReconcileInterval controls how often the drift reconciler runs. Zero
	// takes the default.
	ReconcileInterval time.Duration

	// RequireGraph refuses to start when an entity declares a graph node but
	// no graph adapter is configured.
	//
	// Fabriq's own behavior there is to skip silently: no error, no warning,
	// the entity is simply absent from the graph. For a project whose
	// architectural claim is that one write yields a consistent graph view,
	// booting into a state where that is quietly untrue is worse than not
	// booting.
	RequireGraph bool

	// LifecycleHooks run inside the write transaction, after the row and its
	// outbox event are staged and before commit. Returning an error rolls the
	// whole command back.
	//
	// This is the field forgeext cannot populate, and the reason this package
	// exists.
	LifecycleHooks []command.LifecycleHook

	// Options are passed through to fabriq.Open verbatim, for seams this
	// config does not name explicitly.
	Options []fabriq.Option
}

// DefaultTenant is the single tenant identifier blackstart operates under.
const DefaultTenant = "blackstart"

// DefaultConfig returns the configuration a development host starts with.
func DefaultConfig() Config {
	return Config{
		Tenant:       DefaultTenant,
		RunWorker:    true,
		RequireGraph: true,
	}
}

// Option configures the extension.
type Option func(*Config)

// WithConfig replaces the whole configuration.
func WithConfig(cfg Config) Option {
	return func(c *Config) { *c = cfg }
}

// WithTenant sets the tenant identifier.
func WithTenant(id string) Option {
	return func(c *Config) { c.Tenant = id }
}

// WithGroveDatabase names the grove handle to borrow.
func WithGroveDatabase(name string) Option {
	return func(c *Config) { c.GroveDatabase = name }
}

// WithWorker controls whether this process runs the outbox relay.
func WithWorker(enabled bool) Option {
	return func(c *Config) { c.RunWorker = enabled }
}

// WithLifecycleHook adds hooks that run inside the write transaction.
func WithLifecycleHook(hooks ...command.LifecycleHook) Option {
	return func(c *Config) { c.LifecycleHooks = append(c.LifecycleHooks, hooks...) }
}

// WithFabriqOptions passes options through to fabriq.Open.
func WithFabriqOptions(opts ...fabriq.Option) Option {
	return func(c *Config) { c.Options = append(c.Options, opts...) }
}

// WithRequireGraph controls the startup assertion on graph configuration.
func WithRequireGraph(required bool) Option {
	return func(c *Config) { c.RequireGraph = required }
}
