package source_test

import (
	"testing"

	"github.com/xraph/blackstart/extensions/geo/source"
)

// Both fetchers are Fetchers, which is what lets a command be handed one. The
// assertion is compile-time; the test exists so removing the interface breaks
// here rather than at the call site.
func TestBothSourcesAreFetchers(t *testing.T) {
	var fetchers = []source.Fetcher{
		source.Overpass{},
		source.ArcGISLayer{URL: "https://example.invalid/0"},
	}

	if len(fetchers) != 2 {
		t.Fatal("both sources should satisfy Fetcher")
	}
}
