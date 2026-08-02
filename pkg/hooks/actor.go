// Package hooks provides the acting identity and the domain event bus.
//
// Forge has neither. It carries authentication state as untyped values under
// string keys — ctx.Get("user"), ctx.Get("auth") — set by interceptors, and
// offers no hook dispatcher at all. Both are load-bearing here: the audit
// extension must record who did a thing, and several extensions need to react
// to events they did not cause.
//
// So blackstart owns them. All knowledge of Forge's string-key convention is
// confined to forgebridge.go, so that when Forge grows a typed principal, one
// function changes rather than every call site.
//
// Two kinds of event travel by two different routes, and conflating them is a
// mistake worth avoiding:
//
//   - Entity mutations are audited by a Fabriq LifecycleHook, inside the write
//     transaction. If the audit write fails the domain write rolls back, so no
//     gap is structurally possible.
//   - Domain events — an import finished, a model was promoted, a solve
//     completed — travel on the Bus in this package. It is out of transaction
//     and best-effort.
//
// The Bus is not an audit trail. See a private design note.
package hooks

import (
	"context"
	"fmt"
)

// ActorKind classifies who or what performed an action.
type ActorKind uint8

const (
	// KindSystem is the zero value, and means the action had no attributable
	// actor: a startup task, a scheduled job, a migration.
	//
	// It is the zero value deliberately. An action with no actor recorded
	// should be visibly attributed to the system rather than fail, because the
	// alternative — a write that errors because nobody set an actor — turns
	// every background task into a place where auth plumbing must be
	// remembered. A visibly wrong attribution is discoverable; a refused write
	// in a cron job at 3am is not.
	KindSystem ActorKind = iota

	// KindUser is a human, identified by their identity extension record.
	KindUser

	// KindService is another system authenticating with its own credential.
	KindService

	// KindImport is a data ingest. The actor identifier names the dataset
	// version, so a row's provenance and its audit entry agree.
	KindImport
)

// String renders the kind for logs and audit records.
func (k ActorKind) String() string {
	switch k {
	case KindSystem:
		return "system"
	case KindUser:
		return "user"
	case KindService:
		return "service"
	case KindImport:
		return "import"
	default:
		return fmt.Sprintf("kind(%d)", uint8(k))
	}
}

// ActorID identifies who performed an action.
type ActorID struct {
	Kind ActorKind
	ID   string
}

// System is the actor for unattributed work.
//
//nolint:gochecknoglobals // a constant value; ActorID is not a valid const type
var System = ActorID{Kind: KindSystem, ID: "system"}

// User builds an actor for a human.
func User(id string) ActorID { return ActorID{Kind: KindUser, ID: id} }

// Service builds an actor for a machine caller.
func Service(name string) ActorID { return ActorID{Kind: KindService, ID: name} }

// Import builds an actor for a data ingest, identified by dataset version.
func Import(datasetVersionID string) ActorID {
	return ActorID{Kind: KindImport, ID: datasetVersionID}
}

// IsSystem reports whether this is unattributed work.
func (a ActorID) IsSystem() bool { return a.Kind == KindSystem }

// String renders the actor as "kind:id", which is the form stored in audit
// records and printed in logs.
func (a ActorID) String() string {
	if a.ID == "" {
		return a.Kind.String()
	}

	return a.Kind.String() + ":" + a.ID
}

type actorKey struct{}

// WithActor attaches an acting identity.
func WithActor(ctx context.Context, a ActorID) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

// ActorFromContext returns the acting identity, defaulting to System.
//
// It cannot fail, by design. An audit record attributed to the system is
// honest and reviewable; a write that fails because nobody threaded an actor
// through is an outage. Paths that require a real actor check IsSystem
// explicitly rather than relying on this to refuse.
func ActorFromContext(ctx context.Context) ActorID {
	if a, ok := ctx.Value(actorKey{}).(ActorID); ok {
		return a
	}

	return System
}
