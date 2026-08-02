package hooks_test

import (
	"context"
	"testing"

	"github.com/xraph/chronicle"
	"github.com/xraph/chronicle/audit"
	"github.com/xraph/chronicle/store"
	"github.com/xraph/chronicle/store/memory"

	"github.com/xraph/blackstart/pkg/hooks"
)

// auditTrail stands up a real chronicle over its in-memory store.
//
// The real thing rather than a fake emitter: chronicle's builder API is what
// this bridge is written against, and a fake would let a misuse of it pass.
func auditTrail(t *testing.T) *chronicle.Chronicle {
	t.Helper()

	c, err := chronicle.New(chronicle.WithStore(store.NewAdapter(memory.New())))
	if err != nil {
		t.Fatalf("building a chronicle: %v", err)
	}

	return c
}

// query asks for everything, newest first.
func query() *audit.Query {
	return &audit.Query{Limit: 10, Order: "desc"}
}

// An extension that emits an event is audited without knowing an audit trail
// exists. An extension that had to remember to call an auditor is one that will
// eventually forget.
func TestEveryEventOnTheBusReachesTheAuditTrail(t *testing.T) {
	c := auditTrail(t)
	bus := hooks.NewBus()
	bus.SubscribeAll(hooks.NewAuditSubscriber(c).Record)

	bus.Emit(t.Context(), hooks.Event{
		Name:      "powerflow.solved",
		Extension: "powerflow",
		Entity:    "PowerFlowSolution",
		EntityID:  "pfsol_123",
		Op:        hooks.OpCreate,
	})

	result, err := c.Query(t.Context(), query())
	if err != nil {
		t.Fatalf("querying the audit trail: %v", err)
	}

	if len(result.Events) != 1 {
		t.Fatalf("got %d audit records, want one", len(result.Events))
	}

	recorded := result.Events[0]

	if recorded.Action != "powerflow.solved" {
		t.Errorf("action %q", recorded.Action)
	}

	if recorded.Resource != "PowerFlowSolution" || recorded.ResourceID != "pfsol_123" {
		t.Errorf("the record names %s/%s", recorded.Resource, recorded.ResourceID)
	}
}

// Chronicle groups by category in its compliance reports, so the category
// decides what a reviewer sees grouped together. "Everything the topology
// extension did" is a question people ask; "everything that was a create" is
// not.
func TestTheCategoryIsTheEmittingExtension(t *testing.T) {
	c := auditTrail(t)
	subscriber := hooks.NewAuditSubscriber(c)

	if err := subscriber.Record(t.Context(), hooks.Event{
		Name:      "datasource.created",
		Extension: "datasource",
		Entity:    "Source",
		EntityID:  "dsrc_1",
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	result, err := c.Query(t.Context(), query())
	if err != nil {
		t.Fatalf("querying: %v", err)
	}

	if result.Events[0].Category != "datasource" {
		t.Errorf("category %q, want the emitting extension", result.Events[0].Category)
	}
}

// An event with no name would produce a record nobody can search for. Falling
// back to the operation is not much, but it beats an empty string in a
// compliance export.
func TestAnUnnamedEventStillGetsAnAction(t *testing.T) {
	c := auditTrail(t)

	if err := hooks.NewAuditSubscriber(c).Record(t.Context(), hooks.Event{
		Entity:   "Thing",
		EntityID: "thing_1",
		Op:       hooks.OpUpdate,
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	result, err := c.Query(t.Context(), query())
	if err != nil {
		t.Fatalf("querying: %v", err)
	}

	if result.Events[0].Action == "" {
		t.Error("an unnamed event produced a record with no action")
	}
}

// The actor has to reach the trail, or the audit answers "what happened"
// without "who did it" — which is most of what an audit is for.
func TestTheActorReachesTheTrail(t *testing.T) {
	c := auditTrail(t)

	if err := hooks.NewAuditSubscriber(c).Record(t.Context(), hooks.Event{
		Name:      "thing.done",
		Extension: "test",
		Entity:    "Thing",
		EntityID:  "thing_1",
		Actor:     hooks.User("usr_42"),
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	result, err := c.Query(t.Context(), query())
	if err != nil {
		t.Fatalf("querying: %v", err)
	}

	if result.Events[0].UserID != "usr_42" {
		t.Errorf("the record names user %q", result.Events[0].UserID)
	}
}

// A host with no audit trail configured is a legitimate configuration — bsctl
// is one — and must not panic on every event.
func TestANilEmitterIsHarmless(t *testing.T) {
	subscriber := hooks.NewAuditSubscriber(nil)

	if err := subscriber.Record(context.Background(), hooks.Event{Name: "x"}); err != nil {
		t.Errorf("recording with no emitter: %v", err)
	}
}
