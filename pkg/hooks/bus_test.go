package hooks

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestEmitReachesNamedSubscribers(t *testing.T) {
	t.Parallel()

	bus := NewBus()

	var got []string

	bus.Subscribe("import.completed", func(_ context.Context, e Event) error {
		got = append(got, "first:"+e.EntityID)

		return nil
	})
	bus.Subscribe("import.completed", func(_ context.Context, e Event) error {
		got = append(got, "second:"+e.EntityID)

		return nil
	})
	bus.Subscribe("solve.finished", func(context.Context, Event) error {
		got = append(got, "wrong event")

		return nil
	})

	bus.Emit(t.Context(), Event{Name: "import.completed", EntityID: "dsv_01"})

	if len(got) != 2 {
		t.Fatalf("expected 2 subscribers to fire, got %d: %v", len(got), got)
	}

	// Synchronous dispatch means registration order is delivery order.
	if got[0] != "first:dsv_01" || got[1] != "second:dsv_01" {
		t.Errorf("subscribers fired out of registration order: %v", got)
	}
}

func TestSubscribeAllReceivesEverything(t *testing.T) {
	t.Parallel()

	bus := NewBus()

	var seen []string

	bus.SubscribeAll(func(_ context.Context, e Event) error {
		seen = append(seen, e.Name)

		return nil
	})

	bus.Emit(t.Context(), Event{Name: "a"})
	bus.Emit(t.Context(), Event{Name: "b"})

	if len(seen) != 2 || seen[0] != "a" || seen[1] != "b" {
		t.Errorf("catch-all subscriber saw %v, want [a b]", seen)
	}
}

// A subscriber's failure must not affect the emitter or the other
// subscribers. A listener that needs its failure to matter belongs in the
// write transaction, not on this bus.
func TestASubscriberErrorDoesNotStopTheOthers(t *testing.T) {
	t.Parallel()

	var (
		reported []error
		reached  int
	)

	bus := NewBus(WithErrorHandler(func(_ context.Context, _ Event, err error) {
		reported = append(reported, err)
	}))

	boom := errors.New("subscriber failed")

	bus.Subscribe("x", func(context.Context, Event) error { return boom })
	bus.Subscribe("x", func(context.Context, Event) error {
		reached++

		return nil
	})

	bus.Emit(t.Context(), Event{Name: "x"})

	if reached != 1 {
		t.Error("a failing subscriber prevented a later one from running")
	}

	if len(reported) != 1 || !errors.Is(reported[0], boom) {
		t.Errorf("the error was not reported: %v", reported)
	}
}

// One bad listener must not take down the write that notified it.
func TestAPanickingSubscriberIsContained(t *testing.T) {
	t.Parallel()

	var reported []error

	bus := NewBus(WithErrorHandler(func(_ context.Context, _ Event, err error) {
		reported = append(reported, err)
	}))

	var reached bool

	bus.Subscribe("x", func(context.Context, Event) error {
		panic("subscriber exploded")
	})
	bus.Subscribe("x", func(context.Context, Event) error {
		reached = true

		return nil
	})

	// Must not panic out of Emit.
	bus.Emit(t.Context(), Event{Name: "x"})

	if !reached {
		t.Error("a panicking subscriber prevented a later one from running")
	}

	if len(reported) != 1 {
		t.Fatalf("the panic was not reported: %v", reported)
	}

	var pe *PanicError
	if !errors.As(reported[0], &pe) {
		t.Errorf("expected a PanicError, got %T", reported[0])
	}
}

func TestEmitFillsInTheActorFromContext(t *testing.T) {
	t.Parallel()

	bus := NewBus()

	var got ActorID

	bus.Subscribe("x", func(_ context.Context, e Event) error {
		got = e.Actor

		return nil
	})

	ctx := WithActor(t.Context(), User("usr_01j9"))
	bus.Emit(ctx, Event{Name: "x"})

	if got.Kind != KindUser || got.ID != "usr_01j9" {
		t.Errorf("Emit did not carry the context actor: %s", got)
	}
}

func TestEmitPreservesAnExplicitActor(t *testing.T) {
	t.Parallel()

	bus := NewBus()

	var got ActorID

	bus.Subscribe("x", func(_ context.Context, e Event) error {
		got = e.Actor

		return nil
	})

	ctx := WithActor(t.Context(), User("from-context"))
	bus.Emit(ctx, Event{Name: "x", Actor: Import("dsv_01")})

	if got.Kind != KindImport {
		t.Errorf("Emit overwrote an explicitly-set actor: %s", got)
	}
}

func TestEmitStampsTheTime(t *testing.T) {
	t.Parallel()

	bus := NewBus()

	var got time.Time

	bus.Subscribe("x", func(_ context.Context, e Event) error {
		got = e.At

		return nil
	})

	bus.Emit(t.Context(), Event{Name: "x"})

	if got.IsZero() {
		t.Error("Emit did not stamp a time")
	}

	explicit := time.Date(2026, 2, 14, 3, 0, 0, 0, time.UTC)
	bus.Emit(t.Context(), Event{Name: "x", At: explicit})

	if !got.Equal(explicit) {
		t.Errorf("Emit overwrote an explicit time: %s", got)
	}
}

// An event nobody listens for is normal, not an error: extensions emit
// without knowing who is listening, and most events have no subscriber until
// a later phase adds one.
func TestEmittingWithNoSubscribersIsSafe(t *testing.T) {
	t.Parallel()

	NewBus().Emit(t.Context(), Event{Name: "nobody.listening"})
}

func TestNilHandlersAreIgnored(t *testing.T) {
	t.Parallel()

	bus := NewBus()
	bus.Subscribe("x", nil)
	bus.SubscribeAll(nil)

	if got := bus.SubscriberCount("x"); got != 0 {
		t.Errorf("a nil handler was registered: count = %d", got)
	}

	bus.Emit(t.Context(), Event{Name: "x"})
}

func TestSubscriberCount(t *testing.T) {
	t.Parallel()

	bus := NewBus()
	bus.Subscribe("x", func(context.Context, Event) error { return nil })
	bus.Subscribe("x", func(context.Context, Event) error { return nil })
	bus.Subscribe("y", func(context.Context, Event) error { return nil })
	bus.SubscribeAll(func(context.Context, Event) error { return nil })

	if got := bus.SubscriberCount("x"); got != 3 {
		t.Errorf("SubscriberCount(x) = %d, want 3 (two named plus one catch-all)", got)
	}

	if got := bus.SubscriberCount("z"); got != 1 {
		t.Errorf("SubscriberCount(z) = %d, want 1 (the catch-all)", got)
	}
}

// Subscription happens at startup and emission happens on every write, so the
// two must not race.
func TestConcurrentEmitAndSubscribeAreSafe(t *testing.T) {
	t.Parallel()

	bus := NewBus()

	var wg sync.WaitGroup

	for range 50 {
		wg.Add(2)

		go func() {
			defer wg.Done()

			bus.Emit(context.Background(), Event{Name: "x"})
		}()

		go func() {
			defer wg.Done()

			bus.Subscribe("x", func(context.Context, Event) error { return nil })
		}()
	}

	wg.Wait()
}

// The default error handler must not be nil, or the zero-config bus panics on
// the first failing subscriber.
func TestTheDefaultErrorHandlerIsSafe(t *testing.T) {
	t.Parallel()

	bus := NewBus()
	bus.Subscribe("x", func(context.Context, Event) error {
		return errors.New("boom")
	})

	bus.Emit(t.Context(), Event{Name: "x"})
}

func TestWithErrorHandlerIgnoresNil(t *testing.T) {
	t.Parallel()

	bus := NewBus(WithErrorHandler(nil))
	bus.Subscribe("x", func(context.Context, Event) error {
		return errors.New("boom")
	})

	bus.Emit(t.Context(), Event{Name: "x"})
}
