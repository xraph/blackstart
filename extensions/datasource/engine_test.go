package datasource_test

import (
	"context"
	"errors"
	"testing"

	"github.com/xraph/blackstart/extensions/datasource"
	"github.com/xraph/blackstart/extensions/datasource/feed"
	dsid "github.com/xraph/blackstart/extensions/datasource/id"
	"github.com/xraph/blackstart/extensions/datasource/plugin"
	"github.com/xraph/blackstart/extensions/datasource/source"
	"github.com/xraph/blackstart/extensions/datasource/store/memory"
	"github.com/xraph/blackstart/pkg/hooks"
	"github.com/xraph/blackstart/pkg/id"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

// The engine is exercised against the in-memory store rather than a
// hand-written double. That store honors the temporal context and enforces
// the same uniqueness rules as the real one, so a test passing here is
// evidence about the real system rather than about a mock.
func newEngine(t *testing.T) (*datasource.Engine, *memory.Store) {
	t.Helper()

	s := memory.New()

	e, err := datasource.NewEngine(s, nil, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	return e, s
}

func aSource(slug string) source.Source {
	return source.Source{
		Slug:     slug,
		Name:     "Source " + slug,
		Kind:     source.KindWeather,
		Provides: "test data",
		License:  "public domain",
	}
}

func TestNewEngineRequiresAStore(t *testing.T) {
	t.Parallel()

	if _, err := datasource.NewEngine(nil, nil, nil); err == nil {
		t.Error("built an engine with no store")
	}
}

// The identifier is minted by the engine so an API client never has to, and it
// is always set before the store sees it — Fabriq mints a bare ULID for an
// empty aggregate id, losing the type prefix the platform reads off it.
func TestCreateSourceMintsATypedIdentifier(t *testing.T) {
	t.Parallel()

	e, _ := newEngine(t)

	got, err := e.CreateSource(t.Context(), aSource("noaa"))
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	if got.ID.IsZero() {
		t.Fatal("the engine did not mint an identifier")
	}

	if got.ID.Prefix() != dsid.Source {
		t.Errorf("minted a %q identifier, want a source", got.ID.Prefix())
	}
}

func TestCreateSourceRespectsASuppliedIdentifier(t *testing.T) {
	t.Parallel()

	e, _ := newEngine(t)

	in := aSource("noaa")
	in.ID = dsid.NewSource()

	got, err := e.CreateSource(t.Context(), in)
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	if got.ID != in.ID {
		t.Errorf("the engine replaced a supplied identifier: %s -> %s", in.ID, got.ID)
	}
}

// A feed pointing at a source that does not exist would be invisible in the
// graph and unresolvable in a provenance record. The column is plain text, so
// nothing below the engine would catch it.
func TestCreateFeedRejectsAnUnknownSource(t *testing.T) {
	t.Parallel()

	e, _ := newEngine(t)

	orphan := feed.Feed{
		SourceID: dsid.NewSource(),
		Slug:     "hrrr",
		Name:     "HRRR",
		URL:      "https://example.gov/hrrr",
		Format:   feed.FormatGRIB2,
	}

	_, err := e.CreateFeed(t.Context(), orphan)
	if err == nil {
		t.Fatal("attached a feed to a source that does not exist")
	}

	if !errors.Is(err, source.ErrNotFound) {
		t.Errorf("the error should name the missing source, got %v", err)
	}
}

func TestCreateFeedUnderAKnownSource(t *testing.T) {
	t.Parallel()

	e, _ := newEngine(t)

	src, err := e.CreateSource(t.Context(), aSource("noaa"))
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	got, err := e.CreateFeed(t.Context(), feed.Feed{
		SourceID: src.ID,
		Slug:     "hrrr",
		Name:     "HRRR",
		URL:      "https://example.gov/hrrr",
		Format:   feed.FormatGRIB2,
	})
	if err != nil {
		t.Fatalf("CreateFeed: %v", err)
	}

	if got.ID.Prefix() != dsid.Feed {
		t.Errorf("minted a %q identifier, want a feed", got.ID.Prefix())
	}

	if got.SourceID != src.ID {
		t.Errorf("the feed lost its source: %s", got.SourceID)
	}
}

// Subscribers receive both images because the interesting question is what
// changed. A subscriber that had to re-read to find out would race the next
// write.
type updateWatcher struct {
	before, after source.Source
	calls         int
}

func (w *updateWatcher) OnSourceUpdated(_ context.Context, b, a source.Source) error {
	w.before, w.after = b, a
	w.calls++

	return nil
}

func TestUpdateSourceEmitsBothImages(t *testing.T) {
	t.Parallel()

	s := memory.New()
	watcher := &updateWatcher{}

	plugins := plugin.NewManager(nil)
	plugins.Register(watcher)

	e, err := datasource.NewEngine(s, plugins, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	created, err := e.CreateSource(t.Context(), aSource("eagle-i"))
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	amended := created
	amended.License = "amended terms"

	if _, err := e.UpdateSource(t.Context(), amended); err != nil {
		t.Fatalf("UpdateSource: %v", err)
	}

	if watcher.calls != 1 {
		t.Fatalf("the subscriber was called %d times, want 1", watcher.calls)
	}

	if watcher.before.License != "public domain" {
		t.Errorf("the prior image is wrong: %q", watcher.before.License)
	}

	if watcher.after.License != "amended terms" {
		t.Errorf("the new image is wrong: %q", watcher.after.License)
	}
}

func TestUpdateRejectsAnUnknownSource(t *testing.T) {
	t.Parallel()

	e, _ := newEngine(t)

	ghost := aSource("ghost")
	ghost.ID = dsid.NewSource()

	if _, err := e.UpdateSource(t.Context(), ghost); !errors.Is(err, source.ErrNotFound) {
		t.Errorf("updating an unknown source should be not-found, got %v", err)
	}
}

// The bus is optional: a CLI invocation has no interest in domain events, and
// requiring one would make every command wire a subscriber it does not want.
func TestANilBusIsAccepted(t *testing.T) {
	t.Parallel()

	e, _ := newEngine(t)

	if _, err := e.CreateSource(t.Context(), aSource("nobus")); err != nil {
		t.Errorf("creating with no bus attached failed: %v", err)
	}
}

func TestEventsReachTheBus(t *testing.T) {
	t.Parallel()

	bus := hooks.NewBus()

	var seen []string

	bus.SubscribeAll(func(_ context.Context, ev hooks.Event) error {
		seen = append(seen, ev.Name)

		return nil
	})

	e, err := datasource.NewEngine(memory.New(), nil, bus)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	src, err := e.CreateSource(t.Context(), aSource("noaa"))
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	if _, err := e.CreateFeed(t.Context(), feed.Feed{
		SourceID: src.ID,
		Slug:     "hrrr",
		Name:     "HRRR",
		URL:      "https://example.gov/hrrr",
		Format:   feed.FormatGRIB2,
	}); err != nil {
		t.Fatalf("CreateFeed: %v", err)
	}

	want := []string{"datasource.source.created", "datasource.feed.created"}
	if len(seen) != len(want) {
		t.Fatalf("saw %v, want %v", seen, want)
	}

	for i, name := range want {
		if seen[i] != name {
			t.Errorf("event %d is %q, want %q", i, seen[i], name)
		}
	}
}

func TestReadsPassThrough(t *testing.T) {
	t.Parallel()

	e, _ := newEngine(t)

	created, err := e.CreateSource(t.Context(), aSource("usgs-3dep"))
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	byID, err := e.GetSource(t.Context(), created.ID)
	if err != nil || byID.ID != created.ID {
		t.Errorf("GetSource returned %v, %v", byID, err)
	}

	bySlug, err := e.GetSourceBySlug(t.Context(), "usgs-3dep")
	if err != nil || bySlug.ID != created.ID {
		t.Errorf("GetSourceBySlug returned %v, %v", bySlug, err)
	}

	listed, err := e.ListSources(t.Context(), source.Filter{}, pkgstore.Page{})
	if err != nil || len(listed) != 1 {
		t.Errorf("ListSources returned %d rows, %v", len(listed), err)
	}

	feeds, err := e.ListFeeds(t.Context(), feed.Filter{}, pkgstore.Page{})
	if err != nil || len(feeds) != 0 {
		t.Errorf("ListFeeds returned %d rows, %v", len(feeds), err)
	}

	if _, err := e.GetFeed(t.Context(), id.Nil); err == nil {
		t.Error("GetFeed accepted a nil identifier")
	}
}
