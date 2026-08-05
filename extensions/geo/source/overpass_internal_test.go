package source

import (
	"strings"
	"testing"
)

// A hand-authored fixture cannot catch a query that never asks Overpass for
// tags in the first place: the fixture supplies tags regardless of what was
// requested, so ParseOverpass passes even when the real query would come back
// untagged. This asserts on the query text itself instead — the thing that
// was actually wrong.

// TestOverpassQueryRequestsNodeTags guards against `out skel qt` creeping back
// in for the node output. `skel` is geometry-only — id and position, no tags
// — and it is the choice that looks right if you're only thinking about
// payload size, because the ways already used `out body` and a bare route
// only needs node positions. But `power` and `material` live in node tags,
// and this importer's whole purpose is reading them, so `skel` silently
// empties every structure's class. Confirmed against the live API: `skel`
// answered 1,132 nodes with 0 tagged; `body` answered the same 1,132 nodes
// with 1,002 tagged.
func TestOverpassQueryRequestsNodeTags(t *testing.T) {
	query := overpassQuery(BoundingBox{MinLon: -97.9, MinLat: 30.2, MaxLon: -97.7, MaxLat: 30.4})

	lines := strings.Split(query, "\n")
	if len(lines) == 0 {
		t.Fatal("overpassQuery returned an empty query")
	}

	last := lines[len(lines)-1]
	if !strings.Contains(last, "out body qt") {
		t.Errorf("node output = %q, want \"out body qt;\" — "+
			"\"out skel qt\" drops the tags Kind and Material are read from", last)
	}

	if strings.Contains(query, "out skel") {
		t.Errorf("query contains \"out skel\": %q", query)
	}
}
