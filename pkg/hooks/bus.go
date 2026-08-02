package hooks

import (
	"context"
	"sync"
	"time"
)

// Handler reacts to an event.
//
// A returned error is reported to the bus's error handler and does not stop
// other subscribers or affect the emitter. A subscriber that needs its failure
// to matter is subscribing to the wrong thing: it should be in the write
// transaction instead.
type Handler func(ctx context.Context, e Event) error

// Bus dispatches domain events to subscribers.
//
// Dispatch is by exact event name. Subscriptions are resolved into per-name
// slices when they are registered, so emitting is a map lookup and a loop —
// no reflection, no filtering, no matching, on a path that runs on every
// write.
//
// The zero Bus is not usable; call NewBus.
type Bus struct {
	mu      sync.RWMutex
	byName  map[string][]Handler
	all     []Handler
	onError func(ctx context.Context, e Event, err error)
}

// Option configures a Bus.
type Option func(*Bus)

// WithErrorHandler sets the callback invoked when a subscriber returns an
// error. The default discards them, which is deliberate for the zero-config
// case but is not what a running service should do — the host wires this to
// its logger.
func WithErrorHandler(fn func(ctx context.Context, e Event, err error)) Option {
	return func(b *Bus) {
		if fn != nil {
			b.onError = fn
		}
	}
}

// NewBus builds a bus.
func NewBus(opts ...Option) *Bus {
	b := &Bus{
		byName:  make(map[string][]Handler),
		onError: func(context.Context, Event, error) {},
	}

	for _, opt := range opts {
		opt(b)
	}

	return b
}

// Subscribe registers a handler for one event name.
func (b *Bus) Subscribe(name string, h Handler) {
	if h == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.byName[name] = append(b.byName[name], h)
}

// SubscribeAll registers a handler for every event.
//
// Intended for cross-cutting subscribers — metrics, tracing, a debug stream —
// rather than for domain logic. A domain subscriber that names its events is
// one whose dependencies are visible.
func (b *Bus) SubscribeAll(h Handler) {
	if h == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.all = append(b.all, h)
}

// Emit dispatches an event to its subscribers, synchronously.
//
// Synchronous is the deliberate choice: it keeps ordering predictable and
// keeps a failure attributable to the emit that caused it. Handlers must
// therefore be quick, and one that needs to do real work should hand off to
// its own queue rather than blocking the emitter.
//
// Emit never returns an error and never panics out. A subscriber that panics
// is recovered and reported, because one bad listener must not take down the
// write that notified it.
func (b *Bus) Emit(ctx context.Context, e Event) {
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}

	if e.Actor == (ActorID{}) {
		e.Actor = ActorFromContext(ctx)
	}

	b.mu.RLock()
	named := b.byName[e.Name]
	all := b.all
	b.mu.RUnlock()

	for _, h := range named {
		b.invoke(ctx, e, h)
	}

	for _, h := range all {
		b.invoke(ctx, e, h)
	}
}

func (b *Bus) invoke(ctx context.Context, e Event, h Handler) {
	defer func() {
		if r := recover(); r != nil {
			b.onError(ctx, e, &PanicError{Value: r})
		}
	}()

	if err := h(ctx, e); err != nil {
		b.onError(ctx, e, err)
	}
}

// SubscriberCount reports how many handlers would receive an event of this
// name. Intended for tests and for a startup log line confirming wiring.
func (b *Bus) SubscriberCount(name string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return len(b.byName[name]) + len(b.all)
}
