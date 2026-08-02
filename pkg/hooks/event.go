package hooks

import "time"

// Op names the kind of change an event describes.
type Op string

const (
	OpCreate Op = "create"
	OpUpdate Op = "update"
	OpDelete Op = "delete"

	// OpRun covers events that are not entity changes at all: an import
	// finished, a solve completed, a model was promoted. These are the
	// majority of what travels on this bus.
	OpRun Op = "run"
)

// Event is a domain event.
//
// It is not an audit record. Audit entries for entity mutations are written
// inside the write transaction by a Fabriq LifecycleHook, where a failed audit
// write rolls back the domain write. This bus is out of transaction and
// best-effort, which is the right shape for "recompute the derived geometry"
// and the wrong shape for "record that this happened".
type Event struct {
	// Name is the routing key, dotted and past tense:
	// "network-model.built", "import.completed", "solve.finished".
	Name string

	// Extension that emitted it.
	Extension string

	// Entity and EntityID identify the subject, when there is one.
	Entity   string
	EntityID string

	// Op classifies the change.
	Op Op

	// Actor is who caused it.
	Actor ActorID

	// At is when it happened.
	At time.Time

	// Payload carries event-specific detail. Subscribers type-assert it, and
	// must tolerate a shape they do not recognize rather than panicking — an
	// emitter may add fields without knowing who is listening.
	Payload any
}

// PanicError wraps a value recovered from a panicking subscriber.
type PanicError struct {
	Value any
}

func (e *PanicError) Error() string {
	return "hooks: subscriber panicked: " + toString(e.Value)
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}

	if err, ok := v.(error); ok {
		return err.Error()
	}

	return "non-string panic value"
}
