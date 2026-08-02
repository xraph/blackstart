package memory_test

import (
	"errors"
	"testing"

	"github.com/xraph/blackstart/extensions/datasource/feed"
	dsid "github.com/xraph/blackstart/extensions/datasource/id"
	"github.com/xraph/blackstart/extensions/datasource/source"
	"github.com/xraph/blackstart/extensions/datasource/store/memory"
)

// Feed slugs are unique within a source, not globally: "hrrr" under NOAA and
// "hrrr" under a mirror are different feeds with the same short name.
func TestFeedSlugsAreUniqueWithinASourceOnly(t *testing.T) {
	t.Parallel()

	s := memory.New()

	noaa := newSource("noaa")
	mirror := newSource("mirror")

	for _, src := range []source.Source{noaa, mirror} {
		if err := s.Sources().Create(t.Context(), src); err != nil {
			t.Fatalf("Create source: %v", err)
		}
	}

	if err := s.Feeds().Create(t.Context(), newFeed(noaa.ID, "hrrr")); err != nil {
		t.Fatalf("Create feed: %v", err)
	}

	// Same slug, different source: allowed.
	if err := s.Feeds().Create(t.Context(), newFeed(mirror.ID, "hrrr")); err != nil {
		t.Errorf("the same slug under a different source should be allowed: %v", err)
	}

	// Same slug, same source: rejected.
	err := s.Feeds().Create(t.Context(), newFeed(noaa.ID, "hrrr"))
	if !errors.Is(err, feed.ErrSlugTaken) {
		t.Errorf("a duplicate slug within one source should be rejected, got %v", err)
	}
}

func TestWritesValidate(t *testing.T) {
	t.Parallel()

	s := memory.New()

	invalid := newSource("bad")
	invalid.License = ""

	if err := s.Sources().Create(t.Context(), invalid); err == nil {
		t.Error("the store accepted a source with no license")
	}

	badFeed := newFeed(dsid.NewSource(), "f")
	badFeed.URL = "file:///etc/passwd"

	if err := s.Feeds().Create(t.Context(), badFeed); err == nil {
		t.Error("the store accepted a feed with a file:// URL")
	}
}
