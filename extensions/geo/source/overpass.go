package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xraph/blackstart/extensions/geo/shape"
)

// Overpass reads OpenStreetMap's power layer.
//
// The only public source of per-structure positions at scale. An OSM way
// tagged power=line is a circuit; its ordered nodes are the towers, tagged
// power=tower or power=pole. That maps onto Route → Structure → Span with no
// identifier reconciliation at all, because the relationship is containment
// rather than a join — see the phase-3 data survey, §1.3.
//
// Structures measured around Austin run to a median span of about 207 m, which
// is what makes these real rather than digitising artefacts: a layer whose
// vertices are cartographic has no such bound, and the transmission line layers
// republished from HIFLD reach 40 km between vertices.
//
// The geometry attaches to real OSM circuits and never to ACTIVSg branches. An
// importer using it stands up a real-geometry network beside the synthetic one
// rather than upgrading the synthetic one in place — §1.5.
type Overpass struct {
	// Endpoint is the Overpass API. Empty uses the main public instance.
	Endpoint string

	// Client is the HTTP client. Nil uses one with a generous timeout, because
	// Overpass queues requests under load and a short timeout turns a busy
	// server into a failed import.
	Client *http.Client
}

const (
	defaultOverpass = "https://overpass-api.de/api/interpreter"

	userAgent = "blackstart/0.1 (weather-driven grid outage research; " +
		"https://github.com/xraph/blackstart)"
)

type overpassResponse struct {
	Elements []struct {
		Type  string            `json:"type"`
		ID    int64             `json:"id"`
		Nodes []int64           `json:"nodes"`
		Tags  map[string]string `json:"tags"`
		Lat   float64           `json:"lat"`
		Lon   float64           `json:"lon"`
	} `json:"elements"`
	Remark string `json:"remark"`
}

// Fetch reads every power line whose way intersects the box.
//
// # Errors
//
// Returns an error when the box is not an area, Overpass is unreachable, or it
// answers with a remark — which it uses to report a timeout or a memory limit
// inside an otherwise well-formed 200 response.
func (o Overpass) Fetch(ctx context.Context, box BoundingBox, limit int) ([]Line, error) {
	if err := box.Validate(); err != nil {
		return nil, err
	}

	// `out body` for the ways and `>` for their nodes: the node positions are
	// the structures, and a query returning only ways would give a circuit
	// with no towers on it.
	query := fmt.Sprintf(`[out:json][timeout:120];
way["power"="line"](%g,%g,%g,%g);
out body;
>;
out skel qt;`, box.MinLat, box.MinLon, box.MaxLat, box.MaxLon)

	endpoint := o.Endpoint
	if endpoint == "" {
		endpoint = defaultOverpass
	}

	client := o.Client
	if client == nil {
		client = &http.Client{Timeout: 180 * time.Second}
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(url.Values{"data": {query}}.Encode()))
	if err != nil {
		return nil, fmt.Errorf("source: building the overpass request: %w", err)
	}

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Overpass answers 406 to a client that does not identify itself, and the
	// public instance is free infrastructure maintained by volunteers — an
	// unattributed scraper is both refused and rude. The URL is here so an
	// operator seeing this in their logs can find out what it is.
	request.Header.Set("User-Agent", userAgent)

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("source: fetching overpass: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("source: overpass answered %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("source: reading the overpass response: %w", err)
	}

	lines, err := ParseOverpass(body)
	if err != nil {
		return nil, err
	}

	if limit > 0 && len(lines) > limit {
		lines = lines[:limit]
	}

	return lines, nil
}

// ParseOverpass turns an Overpass response into lines with structures.
//
// Separate from Fetch so parsing is testable against a recorded response
// rather than a live public endpoint under somebody else's load.
func ParseOverpass(body []byte) ([]Line, error) {
	var parsed overpassResponse

	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("source: parsing the overpass response: %w", err)
	}

	// Overpass reports a timeout or memory limit as a remark inside a 200.
	// Ignoring it would turn a truncated answer into an import that looks like
	// a region with fewer lines than it has.
	if parsed.Remark != "" {
		return nil, fmt.Errorf("source: overpass returned a remark: %s", parsed.Remark)
	}

	nodes := make(map[int64]shape.Point, len(parsed.Elements))

	for _, element := range parsed.Elements {
		if element.Type == "node" {
			nodes[element.ID] = shape.Point{Lon: element.Lon, Lat: element.Lat}
		}
	}

	out := make([]Line, 0)

	for _, element := range parsed.Elements {
		if element.Type != "way" || element.Tags["power"] != "line" {
			continue
		}

		points := make(shape.Line, 0, len(element.Nodes))

		for _, ref := range element.Nodes {
			point, ok := nodes[ref]
			if !ok {
				// A way referencing a node the response did not carry is
				// truncated. Dropping the point would silently shorten the
				// span either side of it and move a structure that is really
				// there, so the whole way is skipped.
				points = nil

				break
			}

			points = append(points, point)
		}

		if len(points) < 2 {
			continue
		}

		out = append(out, Line{
			ExternalID: "way/" + strconv.FormatInt(element.ID, 10),
			Source:     "openstreetmap",
			// Path and Structures are the same points, and that is the whole
			// claim: in OSM the vertices of a power line *are* its towers, so
			// there is nothing to interpolate between them.
			Path:       points,
			Structures: points,
			VoltageKV:  voltageKV(element.Tags["voltage"]),
			Owner:      element.Tags["operator"],
		})
	}

	return out, nil
}

// voltageKV reads OSM's voltage tag, which is in volts and may list several
// circuits on one tower as "345000;138000".
func voltageKV(tag string) float64 {
	if tag == "" {
		return 0
	}

	first, _, _ := strings.Cut(tag, ";")

	volts, err := strconv.ParseFloat(strings.TrimSpace(first), 64)
	if err != nil {
		return 0
	}

	return volts / 1000
}
