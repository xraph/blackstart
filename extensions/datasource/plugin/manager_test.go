package plugin_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/xraph/blackstart/extensions/datasource/feed"
	dsid "github.com/xraph/blackstart/extensions/datasource/id"
	"github.com/xraph/blackstart/extensions/datasource/plugin"
	"github.com/xraph/blackstart/extensions/datasource/source"
)

// everything implements all four hooks.
type everything struct {
	mu      sync.Mutex
	seen    []string
	failOn  string
	panicOn string
}

func (e *everything) record(event string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.seen = append(e.seen, event)

	if e.panicOn == event {
		panic("subscriber exploded on " + event)
	}

	if e.failOn == event {
		return errors.New("subscriber failed on " + event)
	}

	return nil
}

func (e *everything) OnSourceCreated(context.Context, source.Source) error {
	return e.record("source.created")
}

func (e *everything) OnSourceUpdated(context.Context, source.Source, source.Source) error {
	return e.record("source.updated")
}

func (e *everything) OnFeedCreated(context.Context, feed.Feed) error {
	return e.record("feed.created")
}

func (e *everything) OnFeedUpdated(context.Context, feed.Feed, feed.Feed) error {
	return e.record("feed.updated")
}

// feedOnly implements one hook. Registering it must not be an error: an
// observer that watches only feeds is a legitimate subscriber.
type feedOnly struct{ calls int }

func (f *feedOnly) OnFeedCreated(context.Context, feed.Feed) error {
	f.calls++

	return nil
}

func TestRegisterWiresEveryImplementedHook(t *testing.T) {
	t.Parallel()

	m := plugin.NewManager(nil)
	m.Register(&everything{})

	for _, event := range []string{
		"source.created", "source.updated", "feed.created", "feed.updated",
	} {
		if got := m.Count(event); got != 1 {
			t.Errorf("%s has %d subscribers, want 1", event, got)
		}
	}
}

// One interface per event is the reason this works: a subscriber implements
// what it cares about and nothing else.
func TestASubscriberMayImplementOneHook(t *testing.T) {
	t.Parallel()

	m := plugin.NewManager(nil)
	sub := &feedOnly{}
	m.Register(sub)

	if got := m.Count("feed.created"); got != 1 {
		t.Errorf("feed.created has %d subscribers, want 1", got)
	}

	if got := m.Count("source.created"); got != 0 {
		t.Errorf("source.created has %d subscribers, want 0", got)
	}

	m.EmitSourceCreated(t.Context(), source.Source{})
	m.EmitFeedCreated(t.Context(), feed.Feed{})

	if sub.calls != 1 {
		t.Errorf("the feed-only subscriber was called %d times, want 1", sub.calls)
	}
}

// A subscriber that implements nothing is accepted silently. Erroring would
// make the caller check which events it happens to implement.
func TestRegisteringANonSubscriberIsHarmless(t *testing.T) {
	t.Parallel()

	m := plugin.NewManager(nil)
	m.Register(struct{}{})
	m.Register(nil)

	if got := m.Count("source.created"); got != 0 {
		t.Errorf("a non-subscriber was wired in: %d", got)
	}
}

// These hooks react to a write that already committed. A failing subscriber
// must not make the write look failed, and must not stop the others.
func TestASubscriberErrorIsReportedNotPropagated(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		reported []string
	)

	m := plugin.NewManager(func(_ context.Context, event string, err error) {
		mu.Lock()
		defer mu.Unlock()

		reported = append(reported, event+": "+err.Error())
	})

	failing := &everything{failOn: "source.created"}
	healthy := &everything{}

	m.Register(failing)
	m.Register(healthy)

	m.EmitSourceCreated(t.Context(), source.Source{})

	if len(healthy.seen) != 1 {
		t.Error("a failing subscriber prevented a later one from running")
	}

	if len(reported) != 1 || !strings.Contains(reported[0], "source.created") {
		t.Errorf("the error was not reported with its event: %v", reported)
	}
}

// One bad subscriber must not take down the request that caused the write.
func TestAPanickingSubscriberIsContained(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		reported []error
	)

	m := plugin.NewManager(func(_ context.Context, _ string, err error) {
		mu.Lock()
		defer mu.Unlock()

		reported = append(reported, err)
	})

	m.Register(&everything{panicOn: "feed.created"})

	healthy := &everything{}
	m.Register(healthy)

	// Must not panic out.
	m.EmitFeedCreated(t.Context(), feed.Feed{})

	if len(healthy.seen) != 1 {
		t.Error("a panicking subscriber prevented a later one from running")
	}

	if len(reported) != 1 {
		t.Fatalf("the panic was not reported: %v", reported)
	}

	if !strings.Contains(reported[0].Error(), "panicked") {
		t.Errorf("the report should say it panicked: %v", reported[0])
	}
}

func TestEmitCarriesBothImagesOnUpdate(t *testing.T) {
	t.Parallel()

	var (
		gotBefore source.Source
		gotAfter  source.Source
	)

	m := plugin.NewManager(nil)
	m.Register(updateWatcher{
		fn: func(b, a source.Source) { gotBefore, gotAfter = b, a },
	})

	before := source.Source{ID: dsid.NewSource(), License: "old"}
	after := before
	after.License = "new"

	m.EmitSourceUpdated(t.Context(), before, after)

	if gotBefore.License != "old" || gotAfter.License != "new" {
		t.Errorf("both images should reach the subscriber: before=%q after=%q",
			gotBefore.License, gotAfter.License)
	}
}

type updateWatcher struct {
	fn func(before, after source.Source)
}

func (w updateWatcher) OnSourceUpdated(_ context.Context, b, a source.Source) error {
	w.fn(b, a)

	return nil
}

// The default error handler must not be nil, or a zero-config manager panics
// on the first failing subscriber.
func TestTheDefaultErrorHandlerIsSafe(t *testing.T) {
	t.Parallel()

	m := plugin.NewManager(nil)
	m.Register(&everything{failOn: "source.created"})

	m.EmitSourceCreated(t.Context(), source.Source{})
}

// Registration happens at startup while emission happens on every write.
func TestConcurrentRegisterAndEmitAreSafe(t *testing.T) {
	t.Parallel()

	m := plugin.NewManager(nil)

	var wg sync.WaitGroup

	for range 40 {
		wg.Add(2)

		go func() {
			defer wg.Done()

			m.EmitSourceCreated(context.Background(), source.Source{})
		}()

		go func() {
			defer wg.Done()

			m.Register(&everything{})
		}()
	}

	wg.Wait()
}
