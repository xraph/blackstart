// Package fabriq is the Fabriq-backed implementation of the geo store.
//
// Geometry is written through Fabriq's PostGIS port rather than through
// hand-rolled tables. The port keys on (entity, id) and stores an untyped
// geometry column behind a GiST index, so a span — which is its own entity
// with its own identifier — gets an indexed LineString for free, and
// containment queries come with it through SpatialCoverer.
package fabriq

import (
	"fmt"

	"github.com/xraph/grove"

	"github.com/xraph/blackstart/extensions/geo/asset"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/geo/shape"
)

// Table names, prefixed with the extension so a shared schema stays legible.
const (
	RouteTable = "blackstart_geo_route"
	SpanTable  = "blackstart_geo_span"
)

// Entity names, as they appear in the registry and in every command.
const (
	// RouteEntity is a branch's path over the ground.
	RouteEntity = "geo_route"

	// SpanEntity is one segment of a route. A first-class entity rather than
	// an array on the route, because a span is what fails and what weather is
	// sampled at — and because Fabriq's geometry port keys on (entity, id),
	// so a span with its own identifier gets its own indexed geometry for
	// free.
	SpanEntity = "geo_span"
)

// routeRow is the routes table.
type routeRow struct {
	grove.BaseModel `grove:"table:blackstart_geo_route"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	CarrierID  string  `grove:"carrier_id,notnull"`
	Provenance string  `grove:"provenance,notnull"`
	LengthM    float64 `grove:"length_m,notnull"`
}

// spanRow is the spans table.
//
// GeomWKT holds the well-known text alongside the PostGIS column that Fabriq's
// spatial port writes. Two copies on purpose: the spatial write is direct
// rather than part of the aggregate's transaction, so a crash between them
// would otherwise lose the geometry outright. With the text on the row the
// geometry column is a rebuildable index — `bsctl geo reindex` replays it —
// rather than the only copy.
type spanRow struct {
	grove.BaseModel `grove:"table:blackstart_geo_span"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	RouteID    string  `grove:"route_id,notnull"`
	Sequence   int     `grove:"sequence,notnull"`
	GeomWKT    string  `grove:"geom_wkt,notnull"`
	LengthM    float64 `grove:"length_m,notnull"`
	AzimuthDeg float64 `grove:"azimuth_deg,notnull"`
}

// toDomain maps a stored span back, rebuilding its geometry from the
// well-known text on the row.
func (s spanRow) toDomain() (asset.Span, error) {
	spanID, err := gid.ParseSpan(s.ID)
	if err != nil {
		return asset.Span{}, fmt.Errorf("geo: stored span id %q: %w", s.ID, err)
	}

	routeID, err := gid.ParseRoute(s.RouteID)
	if err != nil {
		return asset.Span{}, fmt.Errorf("geo: stored route id %q: %w", s.RouteID, err)
	}

	geometry, err := shape.ParseLineEWKT(s.GeomWKT)
	if err != nil {
		return asset.Span{}, fmt.Errorf("geo: span %s: %w", s.ID, err)
	}

	return asset.Span{
		ID:         spanID,
		RouteID:    routeID,
		Sequence:   s.Sequence,
		Geometry:   geometry,
		LengthM:    s.LengthM,
		AzimuthDeg: s.AzimuthDeg,
	}, nil
}
