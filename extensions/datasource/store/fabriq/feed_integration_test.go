//go:build integration

package fabriq_test

import (
	"testing"

	"github.com/xraph/blackstart/extensions/datasource/feed"
	dsid "github.com/xraph/blackstart/extensions/datasource/id"
	"github.com/xraph/blackstart/extensions/datasource/source"
	"github.com/xraph/blackstart/pkg/id"
)

// Feed slugs are unique within a source, which the composite index expresses
// and a global one would not.
func TestFeedSlugsCollideOnlyWithinASource(t *testing.T) {
	s := harness(t)
	ctx := t.Context()

	noaa := newSource("noaa")
	mirror := newSource("mirror")

	for _, src := range []source.Source{noaa, mirror} {
		if err := s.Sources().Create(ctx, src); err != nil {
			t.Fatalf("Create source: %v", err)
		}
	}

	if err := s.Feeds().Create(ctx, feedFor(noaa.ID, "hrrr")); err != nil {
		t.Fatalf("Create feed: %v", err)
	}

	if err := s.Feeds().Create(ctx, feedFor(mirror.ID, "hrrr")); err != nil {
		t.Errorf("the same slug under a different source should be allowed: %v", err)
	}

	if err := s.Feeds().Create(ctx, feedFor(noaa.ID, "hrrr")); err == nil {
		t.Error("a duplicate slug within one source was accepted")
	}
}

// feedFor builds a feed belonging to a source.
func feedFor(sourceID id.ID, slug string) feed.Feed {
	return feed.Feed{
		ID:       dsid.NewFeed(),
		SourceID: sourceID,
		Slug:     slug,
		Name:     "Feed " + slug,
		URL:      "https://example.gov/" + slug,
		Format:   feed.FormatGRIB2,
		Cadence:  "hourly",
	}
}
