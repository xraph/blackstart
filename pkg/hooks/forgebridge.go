package hooks

import (
	"context"

	"github.com/xraph/forge"
)

// This file is a quarantine.
//
// Forge has no typed principal. Its interceptors — RequireAuth, EnrichUser and
// the rest — deposit whatever the application's auth layer produced into the
// request context under bare string keys, and every consumer is expected to
// know the key and to type-assert whatever comes back.
//
// Every line of that knowledge lives here and nowhere else. The rest of
// blackstart takes an ActorID. When Forge grows a typed principal — or when
// the key names change, which they may, since nothing declares them — this is
// the only file that needs to change.
//
// Adding another `ctx.Get("something")` anywhere else in this repository is
// the thing this file exists to prevent.

// The keys Forge's interceptors populate. Neither is declared as a constant
// anywhere in Forge; both are read out of its interceptor documentation and
// source. That is precisely why they are pinned in one place here.
const (
	forgeUserKey = "user"
	forgeAuthKey = "auth"
)

// identifiable is satisfied by auth subjects that can name themselves. Several
// shapes are plausible depending on what the host's auth layer installs, so
// the bridge probes rather than assuming.
type identifiable interface {
	GetID() string
}

// subjectNamed is the other common shape.
type subjectNamed interface {
	Subject() string
}

// ActorFromForgeContext derives the acting identity from a Forge request
// context.
//
// It never fails. An unauthenticated request — a health check, a public
// endpoint — yields System, which is honest: the request genuinely had no
// attributable actor. Endpoints that require a human check IsSystem
// themselves; this function's job is to report what is there, not to enforce
// policy.
func ActorFromForgeContext(c forge.Context) ActorID {
	if c == nil {
		return System
	}

	for _, key := range []string{forgeUserKey, forgeAuthKey} {
		if a, ok := actorFromValue(c.Get(key)); ok {
			return a
		}
	}

	return System
}

// ContextWithActorFromForge lifts the actor out of a Forge request context and
// into the standard context that stores and the event bus read.
//
// This is the call every HTTP controller makes before descending into a
// service. It is the seam between Forge's world and blackstart's.
func ContextWithActorFromForge(c forge.Context) context.Context {
	if c == nil {
		return context.Background()
	}

	return WithActor(c.Context(), ActorFromForgeContext(c))
}

// actorFromValue interprets whatever the auth layer deposited.
//
// The shapes are probed in order of specificity. Anything unrecognized returns
// false rather than being coerced: attributing an action to a subject we could
// not actually identify is worse than attributing it to the system, because it
// looks authoritative.
func actorFromValue(v any) (ActorID, bool) {
	switch subject := v.(type) {
	case nil:
		return System, false

	case string:
		if subject == "" {
			return System, false
		}

		return User(subject), true

	case identifiable:
		if got := subject.GetID(); got != "" {
			return User(got), true
		}

	case subjectNamed:
		if got := subject.Subject(); got != "" {
			return User(got), true
		}

	case map[string]any:
		// JWT claims and similar bags. "sub" is the registered claim; "id" and
		// "user_id" are what hand-rolled middleware tends to use.
		for _, field := range []string{"sub", "id", "user_id"} {
			if got, ok := subject[field].(string); ok && got != "" {
				return User(got), true
			}
		}
	}

	return System, false
}
