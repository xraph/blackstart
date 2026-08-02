package hooks

import (
	"context"
	"fmt"

	"github.com/xraph/chronicle"
)

// AuditSubscriber records domain events to an audit trail.
//
// Blackstart does not own an audit log. Chronicle does — hash-chained, with
// per-subject crypto-erasure and compliance reporting — and this is the whole
// of blackstart's part: turning an event into a record. See
// a private design note.
//
// Subscribed to the bus rather than wired into each extension, so an extension
// that emits an event is audited without knowing an audit trail exists. An
// extension that had to remember to call an auditor is an extension that will
// eventually forget.
type AuditSubscriber struct {
	emitter chronicle.Emitter
}

// NewAuditSubscriber builds a subscriber over an emitter.
func NewAuditSubscriber(emitter chronicle.Emitter) *AuditSubscriber {
	return &AuditSubscriber{emitter: emitter}
}

// Record writes one event to the audit trail.
//
// Matches the Handler signature, so it subscribes to every event with
// bus.SubscribeAll.
//
// Errors are returned rather than swallowed, and the bus decides what to do
// with them — which for this bus means logging and continuing, because the
// alternative is an audit failure rolling back a domain write that succeeded.
// That trade is stated in the ADR: where a hard in-transaction guarantee is
// genuinely required, a LifecycleHook writing through tx sits alongside this
// rather than replacing it.
func (s *AuditSubscriber) Record(ctx context.Context, e Event) error {
	if s.emitter == nil {
		return nil
	}

	action := e.Name
	if action == "" {
		// An event with no name would produce an audit record nobody can
		// search for. Naming it after its operation is not much, but it is
		// better than an empty string in a compliance export.
		action = string(e.Op)
	}

	record := s.emitter.
		Info(ctx, action, e.Entity, e.EntityID).
		Category(categoryOf(e)).
		UserID(e.Actor.ID).
		Meta("extension", e.Extension).
		Meta("actor_kind", string(e.Actor.Kind)).
		Meta("op", string(e.Op))

	if err := record.Record(); err != nil {
		return fmt.Errorf("hooks: recording %q to the audit trail: %w", action, err)
	}

	return nil
}

// categoryOf classifies an event for the audit trail.
//
// Chronicle groups by category in its compliance reports, so this decides what
// a reviewer sees grouped together. The extension name is the useful grouping
// here — "everything the topology extension did" is a question people ask, and
// "everything that was a create" is not.
func categoryOf(e Event) string {
	if e.Extension == "" {
		return "blackstart"
	}

	return e.Extension
}
