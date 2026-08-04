package source_test

import (
	"os"
	"testing"

	"github.com/xraph/blackstart/extensions/geo/source"
)

// The ArcGIS fixture is a real response from a mirror of the HIFLD
// transmission layer, trimmed. Recorded rather than hand-written so the parser
// is tested against what the service actually sends — including the attribute
// types it chooses, which are not the ones the schema documentation implies.
func TestParseArcGISReadsRealResponse(t *testing.T) {
	body, err := os.ReadFile("testdata/arcgis-tx.json")
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}

	lines, err := source.ParseArcGIS(body, "https://example.test/FeatureServer/0")
	if err != nil {
		t.Fatalf("ParseArcGIS: %v", err)
	}

	if len(lines) == 0 {
		t.Fatal("no lines parsed from a response with features")
	}

	for _, line := range lines {
		if err := line.Validate(); err != nil {
			t.Errorf("parsed line does not validate: %v", err)
		}

		// A line layer publishes no structures. If this ever becomes
		// non-empty, geo.Import would call the result Surveyed and assert
		// towers nobody recorded.
		if len(line.Structures) != 0 {
			t.Errorf("line %s carries structures; a line layer has none", line.ExternalID)
		}

		if line.VoltageKV <= 0 {
			t.Errorf("line %s has no voltage; the layer publishes one", line.ExternalID)
		}
	}
}

// ArcGIS reports failures inside a 200 body.

// ArcGIS reports failures inside a 200 body.
func TestParseArcGISSurfacesAnErrorDocument(t *testing.T) {
	_, err := source.ParseArcGIS([]byte(`{"error":{"message":"Invalid where clause"}}`), "x")
	if err == nil {
		t.Fatal("a rejected query must not parse as an empty region")
	}
}
