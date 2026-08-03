// Package apiauth guards the platform's own HTTP surface.
//
// Per-controller rather than global, and that is the point rather than a
// limitation: Authsome's own routes have to stay reachable without a
// credential, because logging in is how you get one. A guard applied to
// everything would make the login endpoint require the thing it issues.
//
// The guard is resolved lazily. Extensions and the authentication authority
// start in an order this package should not depend on, so the middleware is
// looked up on the first request rather than at wiring time — which also means
// a controller can declare its guard before Authsome has finished starting.
package apiauth

import (
	"errors"
	"sync"

	"github.com/xraph/forge"
)

// Guard supplies the middleware that protects a controller.
//
// Resolved from the container by each extension that serves an API. A nil
// Guard means no authentication authority is configured, which is a legitimate
// single-operator deployment — see Middlewares.
type Guard struct {
	resolve func() (forge.Middleware, error)

	once sync.Once
	mw   forge.Middleware
	err  error
}

// New builds a guard over a lazily-resolved middleware.
func New(resolve func() (forge.Middleware, error)) *Guard {
	return &Guard{resolve: resolve}
}

// Middlewares returns what a controller should declare.
//
// A nil guard returns nothing, and that is deliberate rather than an
// oversight: blackstart runs unauthenticated by default, as a single-operator
// tool behind whatever the deployment already trusts. What must never happen
// is a guard that exists and does nothing — a configured authority whose
// middleware failed to resolve returns a middleware that refuses every
// request, so the failure is visible as 500s rather than as an open API.
func (g *Guard) Middlewares() []forge.Middleware {
	if g == nil {
		return nil
	}

	g.once.Do(func() { g.mw, g.err = g.resolve() })

	if g.err != nil || g.mw == nil {
		return []forge.Middleware{refuse(g.err)}
	}

	return []forge.Middleware{g.mw}
}

// ErrUnavailable is what a request meets when the authority could not be
// resolved.
var ErrUnavailable = errors.New(
	"apiauth: this deployment configures an authentication authority and its " +
		"middleware could not be resolved, so every request is refused; serving " +
		"unauthenticated would silently open an API that is meant to be closed")

// refuse builds a middleware that fails every request closed.
func refuse(cause error) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			if cause != nil {
				return forge.NewHTTPError(503, ErrUnavailable.Error()+": "+cause.Error())
			}

			return forge.NewHTTPError(503, ErrUnavailable.Error())
		}
	}
}
