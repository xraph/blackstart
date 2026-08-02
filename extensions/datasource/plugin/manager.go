package plugin

import (
	"context"
	"sync"

	"github.com/xraph/blackstart/extensions/datasource/feed"
	"github.com/xraph/blackstart/extensions/datasource/source"
)

// Manager fans events out to subscribers.
//
// Subscribers are type-asserted once, at Register, into per-event slices. Emit
// then walks a slice with no reflection and no assertions — which matters
// because emission is on the write path and registration happens once at
// startup.
type Manager struct {
	mu sync.RWMutex

	sourceCreated []SourceCreated
	sourceUpdated []SourceUpdated
	feedCreated   []FeedCreated
	feedUpdated   []FeedUpdated

	onError func(ctx context.Context, event string, err error)
}

// NewManager builds a manager.
func NewManager(onError func(ctx context.Context, event string, err error)) *Manager {
	if onError == nil {
		onError = func(context.Context, string, error) {}
	}

	return &Manager{onError: onError}
}

// Register wires a subscriber into every event it implements.
//
// A subscriber implementing none is accepted silently rather than rejected:
// an observer that watches only feeds is a legitimate thing to register, and
// erroring on it would make the caller check which events it happens to
// implement.
func (m *Manager) Register(subscriber any) {
	if subscriber == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := subscriber.(SourceCreated); ok {
		m.sourceCreated = append(m.sourceCreated, s)
	}

	if s, ok := subscriber.(SourceUpdated); ok {
		m.sourceUpdated = append(m.sourceUpdated, s)
	}

	if s, ok := subscriber.(FeedCreated); ok {
		m.feedCreated = append(m.feedCreated, s)
	}

	if s, ok := subscriber.(FeedUpdated); ok {
		m.feedUpdated = append(m.feedUpdated, s)
	}
}

// EmitSourceCreated notifies subscribers.
//
// Emission never returns an error and never propagates a panic. These hooks
// are reactions to a write that has already committed; a subscriber that fails
// must not make the write look like it failed, and one that panics must not
// take down the request that caused it. A subscriber whose failure genuinely
// matters belongs in the write transaction, which is what Fabriq's lifecycle
// hook is for.
func (m *Manager) EmitSourceCreated(ctx context.Context, s source.Source) {
	m.mu.RLock()
	subs := m.sourceCreated
	m.mu.RUnlock()

	for _, sub := range subs {
		m.guard(ctx, "source.created", func() error { return sub.OnSourceCreated(ctx, s) })
	}
}

// EmitSourceUpdated notifies subscribers.
func (m *Manager) EmitSourceUpdated(ctx context.Context, before, after source.Source) {
	m.mu.RLock()
	subs := m.sourceUpdated
	m.mu.RUnlock()

	for _, sub := range subs {
		m.guard(ctx, "source.updated", func() error {
			return sub.OnSourceUpdated(ctx, before, after)
		})
	}
}

// EmitFeedCreated notifies subscribers.
func (m *Manager) EmitFeedCreated(ctx context.Context, f feed.Feed) {
	m.mu.RLock()
	subs := m.feedCreated
	m.mu.RUnlock()

	for _, sub := range subs {
		m.guard(ctx, "feed.created", func() error { return sub.OnFeedCreated(ctx, f) })
	}
}

// EmitFeedUpdated notifies subscribers.
func (m *Manager) EmitFeedUpdated(ctx context.Context, before, after feed.Feed) {
	m.mu.RLock()
	subs := m.feedUpdated
	m.mu.RUnlock()

	for _, sub := range subs {
		m.guard(ctx, "feed.updated", func() error {
			return sub.OnFeedUpdated(ctx, before, after)
		})
	}
}

// Count reports how many subscribers would receive an event, for tests and for
// a startup line confirming the wiring.
func (m *Manager) Count(event string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	switch event {
	case "source.created":
		return len(m.sourceCreated)
	case "source.updated":
		return len(m.sourceUpdated)
	case "feed.created":
		return len(m.feedCreated)
	case "feed.updated":
		return len(m.feedUpdated)
	default:
		return 0
	}
}

func (m *Manager) guard(ctx context.Context, event string, fn func() error) {
	defer func() {
		if r := recover(); r != nil {
			m.onError(ctx, event, &panicError{value: r})
		}
	}()

	if err := fn(); err != nil {
		m.onError(ctx, event, err)
	}
}

type panicError struct{ value any }

func (e *panicError) Error() string {
	if s, ok := e.value.(string); ok {
		return "datasource: subscriber panicked: " + s
	}

	if err, ok := e.value.(error); ok {
		return "datasource: subscriber panicked: " + err.Error()
	}

	return "datasource: subscriber panicked"
}
