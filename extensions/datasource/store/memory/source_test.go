package memory_test

import (
	"errors"
	"testing"

	"github.com/xraph/blackstart/extensions/datasource/source"
	"github.com/xraph/blackstart/extensions/datasource/store/memory"
	"github.com/xraph/blackstart/pkg/store"
)

func TestCreateAndGet(t *testing.T) {
	t.Parallel()

	s := memory.New()
	in := newSource("noaa-hrrr")

	if err := s.Sources().Create(t.Context(), in); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Sources().Get(t.Context(), in.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Slug != in.Slug {
		t.Errorf("Get returned %q, want %q", got.Slug, in.Slug)
	}
}

// THE test for this store. A memory store that ignores the temporal context
// lets every service test pass while the real store returns different rows —
// exactly the divergence pkg/bitemporal exists to prevent, and the reason this
// implementation is more than a map.
func TestAnUpdateDoesNotLeakBackwardsInKnowledgeTime(t *testing.T) {
	t.Parallel()

	s := memory.New()
	clockAt(s, 10)

	original := newSource("eagle-i")
	original.License = "original terms"

	if err := s.Sources().Create(asOf(t, 10, 10), original); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// On the 20th the license statement is corrected.
	clockAt(s, 20)

	corrected := original
	corrected.License = "corrected terms"

	if err := s.Sources().Update(asOf(t, 20, 20), corrected); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// A reader as of the 15th must still see the original terms. Anything else
	// means a provenance record written then would resolve a license nobody
	// had yet stated.
	before, err := s.Sources().Get(asOf(t, 15, 15), original.ID)
	if err != nil {
		t.Fatalf("Get as of the 15th: %v", err)
	}

	if before.License != "original terms" {
		t.Errorf("a read as of the 15th returned %q; the correction filed on "+
			"the 20th has leaked backwards", before.License)
	}

	// And a reader today sees the correction.
	after, err := s.Sources().Get(asOf(t, 25, 25), original.ID)
	if err != nil {
		t.Fatalf("Get as of the 25th: %v", err)
	}

	if after.License != "corrected terms" {
		t.Errorf("a present-day read returned %q, want the correction", after.License)
	}
}

// The same property must hold for listings, not just point reads — a listing
// that ignores knowledge time is the easier mistake to make.
func TestListHonoursKnowledgeTime(t *testing.T) {
	t.Parallel()

	s := memory.New()
	clockAt(s, 10)

	in := newSource("early")
	if err := s.Sources().Create(asOf(t, 10, 10), in); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Nothing was known on the 5th.
	got, err := s.Sources().List(asOf(t, 5, 5), source.Filter{}, store.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(got) != 0 {
		t.Errorf("a listing as of the 5th returned %d sources; none existed yet", len(got))
	}

	got, err = s.Sources().List(asOf(t, 15, 15), source.Filter{}, store.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(got) != 1 {
		t.Errorf("a listing as of the 15th returned %d sources, want 1", len(got))
	}
}

func TestGetBySlug(t *testing.T) {
	t.Parallel()

	s := memory.New()
	in := newSource("usgs-3dep")

	if err := s.Sources().Create(t.Context(), in); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Sources().GetBySlug(t.Context(), "usgs-3dep")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}

	if got.ID != in.ID {
		t.Errorf("GetBySlug returned %s, want %s", got.ID, in.ID)
	}

	if _, err := s.Sources().GetBySlug(t.Context(), "nonexistent"); !errors.Is(err, source.ErrNotFound) {
		t.Errorf("a missing slug should be not-found, got %v", err)
	}
}

// A slug is a public handle used on the command line. Two sources sharing one
// would make `--source noaa` ambiguous.
func TestSourceSlugsAreUnique(t *testing.T) {
	t.Parallel()

	s := memory.New()

	first := newSource("duplicate")
	if err := s.Sources().Create(t.Context(), first); err != nil {
		t.Fatalf("Create: %v", err)
	}

	second := newSource("duplicate")

	err := s.Sources().Create(t.Context(), second)
	if !errors.Is(err, source.ErrSlugTaken) {
		t.Errorf("a duplicate slug should be rejected, got %v", err)
	}
}

func TestListFilters(t *testing.T) {
	t.Parallel()

	s := memory.New()

	weather := newSource("weather-one")
	network := newSource("network-one")
	network.Kind = source.KindNetwork

	for _, src := range []source.Source{weather, network} {
		if err := s.Sources().Create(t.Context(), src); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	got, err := s.Sources().List(t.Context(),
		source.Filter{Kind: source.KindNetwork}, store.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(got) != 1 || got[0].Slug != "network-one" {
		t.Errorf("filtering by kind returned %v", got)
	}
}

func TestUpdateRejectsAnUnknownSource(t *testing.T) {
	t.Parallel()

	s := memory.New()

	err := s.Sources().Update(t.Context(), newSource("ghost"))
	if !errors.Is(err, source.ErrNotFound) {
		t.Errorf("updating an unknown source should be not-found, got %v", err)
	}
}
