package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/blackstart/extensions/datasource/feed"
	dsid "github.com/xraph/blackstart/extensions/datasource/id"
	"github.com/xraph/blackstart/extensions/datasource/source"
	"github.com/xraph/blackstart/extensions/datasource/store/memory"
	"github.com/xraph/blackstart/pkg/bitemporal"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/store"
)

func day(n int) bitemporal.Instant {
	return bitemporal.At(time.Date(2026, 2, n, 0, 0, 0, 0, time.UTC))
}

// asOf returns a context reading the world at validDay using knowledge as of
// knownDay.
func asOf(t *testing.T, validDay, knownDay int) context.Context {
	t.Helper()

	ctx, err := bitemporal.WithAsOf(t.Context(),
		bitemporal.PointInTime(day(validDay), day(knownDay)))
	if err != nil {
		t.Fatalf("WithAsOf: %v", err)
	}

	return ctx
}

// clockAt fixes the store's knowledge clock, so a correction lands at a chosen
// instant rather than at wall-clock now.
func clockAt(s *memory.Store, n int) {
	s.SetClock(func() bitemporal.Instant { return day(n) })
}

func newSource(slug string) source.Source {
	return source.Source{
		ID:       dsid.NewSource(),
		Slug:     slug,
		Name:     "Test source " + slug,
		Kind:     source.KindWeather,
		Provides: "test data",
		License:  "public domain",
	}
}

func newFeed(sourceID id.ID, slug string) feed.Feed {
	return feed.Feed{
		ID:       dsid.NewFeed(),
		SourceID: sourceID,
		Slug:     slug,
		Name:     "Test feed " + slug,
		URL:      "https://example.gov/" + slug,
		Format:   feed.FormatCSV,
		Cadence:  "hourly",
	}
}

// Pagination is keyset over identifiers, which sort chronologically. Listings
// must therefore come back in identifier order, or a cursor means nothing.
func TestListIsOrderedAndPaginates(t *testing.T) {
	t.Parallel()

	s := memory.New()

	created := make([]id.ID, 0, 5)

	for i := range 5 {
		in := newSource("src-" + string(rune('a'+i)))
		if err := s.Sources().Create(t.Context(), in); err != nil {
			t.Fatalf("Create: %v", err)
		}

		created = append(created, in.ID)
	}

	first, err := s.Sources().List(t.Context(), source.Filter{}, store.Page{Limit: 2})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(first) != 2 {
		t.Fatalf("first page has %d entries, want 2", len(first))
	}

	if first[0].ID.String() >= first[1].ID.String() {
		t.Error("a page is not in identifier order; a cursor over it means nothing")
	}

	second, err := s.Sources().List(t.Context(),
		source.Filter{}, store.Page{Limit: 2, After: first[1].ID})
	if err != nil {
		t.Fatalf("List page two: %v", err)
	}

	if len(second) != 2 {
		t.Fatalf("second page has %d entries, want 2", len(second))
	}

	if second[0].ID.String() <= first[1].ID.String() {
		t.Error("the second page overlaps the first")
	}

	if len(created) != 5 {
		t.Fatalf("precondition: created %d sources", len(created))
	}
}

func TestLifecycle(t *testing.T) {
	t.Parallel()

	s := memory.New()

	if err := s.Migrate(t.Context()); err != nil {
		t.Errorf("Migrate: %v", err)
	}

	if err := s.Ping(t.Context()); err != nil {
		t.Errorf("Ping: %v", err)
	}

	if err := s.Sources().Create(t.Context(), newSource("x")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	got, err := s.Sources().List(t.Context(), source.Filter{}, store.Page{})
	if err != nil {
		t.Fatalf("List after Close: %v", err)
	}

	if len(got) != 0 {
		t.Errorf("Close left %d sources behind", len(got))
	}

	if s.StoreName() == "" {
		t.Error("StoreName is empty")
	}
}
