package server

import (
	"net/http"

	"github.com/xraph/forge"

	"github.com/xraph/blackstart/pkg/buildinfo"
)

// Health route paths.
//
// /health is the human-facing report; the two Kubernetes-shaped probes are
// separate because they answer different questions. Liveness asks "is this
// process wedged", and answering it with a dependency check is how a database
// blip turns into a restart loop that makes the outage worse. Readiness asks
// "should traffic come here", and there a failing dependency is exactly the
// right reason to say no.
const (
	HealthPath    = "/health"
	LivenessPath  = "/livez"
	ReadinessPath = "/readyz"
)

// healthController exposes the health manager over HTTP.
//
// Forge builds a HealthManager and runs its checks, but only wires an HTTP
// route on its debug server — nothing on the application router. Without this,
// the platform reports its health to nobody, which was true until it was
// checked with curl rather than assumed from the presence of the manager.
type healthController struct {
	app forge.App
}

func (c *healthController) Name() string { return "health" }

func (c *healthController) Routes(r forge.Router) error {
	if err := r.GET(HealthPath, c.report); err != nil {
		return err
	}

	if err := r.GET(LivenessPath, c.live); err != nil {
		return err
	}

	return r.GET(ReadinessPath, c.ready)
}

// report returns the full health report, and a status code that a monitoring
// system can act on without parsing the body.
func (c *healthController) report(ctx forge.Context) error {
	mgr := c.app.HealthManager()
	if mgr == nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]any{
			"status": "unknown",
			"reason": "no health manager",
		})
	}

	rep := mgr.Check(ctx.Context())

	info := buildinfo.Get()

	body := map[string]any{
		"status":  string(mgr.Status()),
		"version": info.Version,
		"commit":  info.Commit,
		// A result produced by an unreproducible build cannot be traced to a
		// commit, and the provenance records will say so. Surfacing it here
		// means an operator can see it without reading the startup log.
		"reproducible": info.Reproducible(),
		"report":       rep,
	}

	return ctx.JSON(statusCode(mgr.Status()), body)
}

// live reports only that the process is running. It deliberately checks
// nothing else.
func (c *healthController) live(ctx forge.Context) error {
	return ctx.JSON(http.StatusOK, map[string]any{"status": "alive"})
}

// ready reports whether this instance should receive traffic, which does
// depend on the datastores.
func (c *healthController) ready(ctx forge.Context) error {
	mgr := c.app.HealthManager()
	if mgr == nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]any{"status": "unknown"})
	}

	// Check runs every registered check and folds the outcome into Status.
	// Forge's llm.txt documents a CheckAll returning an error; the interface
	// it actually exposes is this one, and following the documentation would
	// not have compiled.
	mgr.Check(ctx.Context())

	status := mgr.Status()

	return ctx.JSON(statusCode(status), map[string]any{"status": string(status)})
}

// statusCode maps a health status onto an HTTP code.
//
// Degraded is 200. A load balancer that pulls an instance out for being
// degraded removes capacity at exactly the moment the system is under strain,
// which is the wrong direction — degraded means "working, with less margin",
// and the report body carries the detail for anyone who wants it.
func statusCode(s forge.HealthStatus) int {
	if s == forge.HealthStatusUnhealthy {
		return http.StatusServiceUnavailable
	}

	return http.StatusOK
}
