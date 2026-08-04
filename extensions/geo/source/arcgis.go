package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/xraph/blackstart/extensions/geo/shape"
)

// ArcGISLayer is a feature service publishing transmission lines.
//
// The EIA U.S. Energy Atlas republication of the HIFLD layer, or one of the
// state and agency mirrors of the same data. Whichever is used, the schema is
// HIFLD's: OWNER, VOLTAGE, VOLT_CLASS, INFERRED, SUB_1, SUB_2, SOURCE.
//
// What this layer is and is not was measured rather than assumed. Its vertices
// are digitizing points: the median gap is near a real span but the maximum is
// over 40 km, which no conductor crosses unsupported. It therefore yields
// Routed geometry — a real right-of-way with interpolated structures — and
// never Surveyed. See the phase-3 data survey, §1.4.1.
type ArcGISLayer struct {
	// URL is the feature service layer, without a trailing /query.
	URL string

	// Client is the HTTP client. Nil uses a client with a timeout, because a
	// public feature service that stops responding should not hang an import
	// indefinitely.
	Client *http.Client
}

type arcgisResponse struct {
	Features []struct {
		Attributes map[string]any `json:"attributes"`
		Geometry   struct {
			Paths [][][2]float64 `json:"paths"`
		} `json:"geometry"`
	} `json:"features"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Fetch reads every line intersecting the box.
//
// # Errors
//
// Returns an error when the box is not an area, the service is unreachable, or
// it answers with an error document — which it does with a 200, so the status
// code alone is not enough to know the request worked.
func (a ArcGISLayer) Fetch(ctx context.Context, box BoundingBox, limit int) ([]Line, error) {
	if err := box.Validate(); err != nil {
		return nil, err
	}

	query := url.Values{
		"where":          {"1=1"},
		"geometry":       {fmt.Sprintf("%g,%g,%g,%g", box.MinLon, box.MinLat, box.MaxLon, box.MaxLat)},
		"geometryType":   {"esriGeometryEnvelope"},
		"inSR":           {"4326"},
		"outSR":          {"4326"},
		"spatialRel":     {"esriSpatialRelIntersects"},
		"outFields":      {"OBJECTID,ID,OWNER,VOLTAGE,VOLT_CLASS,SOURCE"},
		"returnGeometry": {"true"},
		"f":              {"json"},
	}

	if limit > 0 {
		query.Set("resultRecordCount", strconv.Itoa(limit))
	}

	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL+"/query?"+query.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("source: building the arcgis request: %w", err)
	}

	// G704 as in overpass.go: the layer URL is supplied by the operator running
	// the import, because HIFLD's own portal is gone and which mirror to trust
	// is their call rather than this code's. See the phase-3 data survey, §1.4.
	//nolint:gosec // fetching a caller-named public endpoint is the feature
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("source: fetching %s: %w", a.URL, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("source: %s answered %d", a.URL, response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("source: reading the arcgis response: %w", err)
	}

	return ParseArcGIS(body, a.URL)
}

// ParseArcGIS turns a feature service response into lines.
//
// Separate from Fetch so the parsing is testable against a recorded response
// rather than a live public endpoint, which would make the suite depend on
// somebody else's uptime.
func ParseArcGIS(body []byte, origin string) ([]Line, error) {
	var parsed arcgisResponse

	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("source: parsing the arcgis response: %w", err)
	}

	// ArcGIS reports failures inside a 200 body. Treating the status code as
	// the answer would turn a rejected query into an empty import that looks
	// like a region with no transmission in it.
	if parsed.Error != nil {
		return nil, fmt.Errorf("source: arcgis rejected the query: %s", parsed.Error.Message)
	}

	out := make([]Line, 0, len(parsed.Features))

	for index, feature := range parsed.Features {
		id := attrString(feature.Attributes, "ID")
		if id == "" {
			id = attrString(feature.Attributes, "OBJECTID")
		}

		for pathIndex, path := range feature.Geometry.Paths {
			points := make(shape.Line, 0, len(path))
			for _, pair := range path {
				points = append(points, shape.Point{Lon: pair[0], Lat: pair[1]})
			}

			if len(points) < 2 {
				// A one-point path is not a line. Dropped rather than stored,
				// since it has no length, no bearing and nothing to fail.
				continue
			}

			external := id
			if external == "" {
				external = fmt.Sprintf("feature-%d", index)
			}

			if len(feature.Geometry.Paths) > 1 {
				// A multipart feature is several disjoint runs of one circuit.
				// Each becomes its own line, and the suffix keeps them
				// distinguishable when tracing a route back to its source.
				external = fmt.Sprintf("%s#%d", external, pathIndex)
			}

			out = append(out, Line{
				ExternalID: external,
				Source:     origin,
				Path:       points,
				VoltageKV:  attrFloat(feature.Attributes, "VOLTAGE"),
				Owner:      attrString(feature.Attributes, "OWNER"),
			})
		}
	}

	return out, nil
}

func attrString(attrs map[string]any, key string) string {
	switch v := attrs[key].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return ""
	}
}

func attrFloat(attrs map[string]any, key string) float64 {
	switch v := attrs[key].(type) {
	case float64:
		return v
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0
		}

		return f
	default:
		return 0
	}
}
