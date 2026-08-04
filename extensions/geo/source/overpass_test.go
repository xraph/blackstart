package source_test

import (
	"testing"

	"github.com/xraph/blackstart/extensions/geo/source"
)

func TestParseOverpassPairsWaysWithTheirNodes(t *testing.T) {
	body := []byte(`{"elements":[
      {"type":"way","id":318827441,"nodes":[1,2,3],
       "tags":{"power":"line","voltage":"345000","operator":"Oncor"}},
      {"type":"node","id":1,"lat":34.5400,"lon":-101.7600},
      {"type":"node","id":2,"lat":34.5380,"lon":-101.7625},
      {"type":"node","id":3,"lat":34.5360,"lon":-101.7650}
    ]}`)

	lines, err := source.ParseOverpass(body)
	if err != nil {
		t.Fatalf("ParseOverpass: %v", err)
	}

	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}

	line := lines[0]

	if line.ExternalID != "way/318827441" {
		t.Errorf("external id = %q; a route must be traceable back to its way", line.ExternalID)
	}

	// In OSM the vertices of a power line are its towers. That identity is
	// what makes the import Surveyed rather than Routed.
	if len(line.Structures) != 3 || len(line.Path) != 3 {
		t.Errorf("path %d points, structures %d; both should be the 3 nodes",
			len(line.Path), len(line.Structures))
	}

	if line.VoltageKV != 345 {
		t.Errorf("voltage = %v kV, want 345 — the tag is in volts", line.VoltageKV)
	}
}

// A way whose nodes were not returned is truncated, and a truncated way moves
// a structure that is really there.

// A way whose nodes were not returned is truncated, and a truncated way moves
// a structure that is really there.
func TestParseOverpassSkipsAWayMissingNodes(t *testing.T) {
	body := []byte(`{"elements":[
      {"type":"way","id":1,"nodes":[1,2,99],"tags":{"power":"line"}},
      {"type":"node","id":1,"lat":34.54,"lon":-101.76},
      {"type":"node","id":2,"lat":34.53,"lon":-101.77}
    ]}`)

	lines, err := source.ParseOverpass(body)
	if err != nil {
		t.Fatalf("ParseOverpass: %v", err)
	}

	if len(lines) != 0 {
		t.Errorf("got %d lines; a way missing a node must be skipped, not shortened", len(lines))
	}
}

// Overpass reports a timeout or memory limit as a remark inside a 200.

// Overpass reports a timeout or memory limit as a remark inside a 200.
func TestParseOverpassSurfacesARemark(t *testing.T) {
	_, err := source.ParseOverpass([]byte(`{"elements":[],"remark":"runtime error: Query timed out"}`))
	if err == nil {
		t.Fatal("a truncated answer must not parse as a region with no lines")
	}
}

func TestParseOverpassIgnoresNonPowerWays(t *testing.T) {
	body := []byte(`{"elements":[
      {"type":"way","id":7,"nodes":[1,2],"tags":{"highway":"track"}},
      {"type":"node","id":1,"lat":34.54,"lon":-101.76},
      {"type":"node","id":2,"lat":34.53,"lon":-101.77}
    ]}`)

	lines, err := source.ParseOverpass(body)
	if err != nil {
		t.Fatalf("ParseOverpass: %v", err)
	}

	if len(lines) != 0 {
		t.Errorf("got %d lines from a response with no power lines", len(lines))
	}
}

func TestBoundingBoxRejectsWhatIsNotAnArea(t *testing.T) {
	for _, tc := range []struct {
		name string
		box  source.BoundingBox
	}{
		{"inverted", source.BoundingBox{MinLon: -100, MinLat: 34, MaxLon: -102, MaxLat: 35}},
		{"empty", source.BoundingBox{MinLon: -100, MinLat: 34, MaxLon: -100, MaxLat: 34}},
		{"off the earth", source.BoundingBox{MinLon: -200, MinLat: 34, MaxLon: -100, MaxLat: 35}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.box.Validate(); err == nil {
				t.Error("expected an error")
			}
		})
	}
}
