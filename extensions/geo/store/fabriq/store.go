package fabriq

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/fabriq"

	"github.com/xraph/blackstart/extensions/geo/asset"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/geo/shape"
	"github.com/xraph/blackstart/extensions/geo/store"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/store/fabriqstore"
)

// Store is the Fabriq-backed composite.
type Store struct {
	base *fabriqstore.Base
}

// New builds the store over a Fabriq facade.
func New(fab *fabriq.Fabriq, tenant string) (*Store, error) {
	base, err := fabriqstore.New(fab, tenant)
	if err != nil {
		return nil, fmt.Errorf("geo: building the store: %w", err)
	}

	return &Store{base: base}, nil
}

// StoreName identifies the store in health output and migration logs.
func (s *Store) StoreName() string { return "geo/fabriq" }

// Migrate is a no-op here; schema changes run through the group in
// migrations.go.
func (s *Store) Migrate(context.Context) error { return nil }

// Ping reports whether the source of truth is reachable.
func (s *Store) Ping(ctx context.Context) error { return s.base.Ping(ctx) }

// Close releases nothing: the Fabriq facade is owned by the fabric extension.
func (s *Store) Close() error { return nil }

// Routes returns the route store.
func (s *Store) Routes() store.RouteStore { return (*routes)(s) }

type routes Store

// Put writes a route and replaces its spans.
//
// The rows go through Fabriq's command path and the geometry goes through its
// spatial port, which is a direct write outside the aggregate's transaction.
// The order matters: rows first, geometry second. A crash between them leaves
// a span whose geometry is missing from the index but present as well-known
// text on its own row, which `bsctl geo reindex` can replay. The reverse order
// would leave indexed geometry for a span that does not exist.
func (r *routes) Put(ctx context.Context, route asset.Route, spans []asset.Span) error {
	if err := route.Validate(); err != nil {
		return err
	}

	// A branch has one route, so a re-trace replaces rather than colliding
	// with the unique index. Re-tracing is a normal thing to do — correcting a
	// span length, or replacing an interpolated path with a surveyed one — and
	// making it an error would mean the only way to correct geometry is to
	// delete it by hand first.
	if err := r.forget(ctx, route.BranchID); err != nil {
		return err
	}

	if err := r.base.Create(ctx, RouteEntity, route.ID, routeRow{
		ID:         route.ID.String(),
		BranchID:   route.BranchID.String(),
		Provenance: string(route.Provenance),
		LengthM:    route.LengthM,
	}); err != nil {
		return err
	}

	for index, span := range spans {
		if err := span.Validate(); err != nil {
			return fmt.Errorf("geo: span %d: %w", index, err)
		}

		if span.RouteID != route.ID {
			return fmt.Errorf(
				"%w: span %d belongs to route %s, not %s",
				bserr.ErrInvalid, index, span.RouteID, route.ID)
		}

		wkt := span.Geometry.EWKT()

		if err := r.base.Create(ctx, SpanEntity, span.ID, spanRow{
			ID:         span.ID.String(),
			RouteID:    span.RouteID.String(),
			Sequence:   span.Sequence,
			GeomWKT:    wkt,
			LengthM:    span.LengthM,
			AzimuthDeg: span.AzimuthDeg,
		}); err != nil {
			return err
		}

		// The meta carries what a spatial query filters on without joining
		// back: "which spans of this route are within 50 km of this storm
		// cell" is one question, and requiring a join to answer it would make
		// the geometry index useless for the query it exists to serve.
		if err := r.base.Spatial(ctx, SpanEntity, span.ID, wkt, shape.SRID,
			map[string]any{"route_id": span.RouteID.String()}); err != nil {
			return err
		}
	}

	return nil
}

// forget removes any existing route for a branch, with its spans and their
// geometry.
//
// Geometry is deleted before the rows that name it. A crash in between leaves
// an indexed geometry for a span that still exists, which the next write
// replaces; the other order would leave geometry pointing at nothing, and
// nothing would ever visit it again to clean it up.
func (r *routes) forget(ctx context.Context, branch id.ID) error {
	existing, err := r.ForBranch(ctx, branch)
	if errors.Is(err, bserr.ErrNotFound) {
		return nil
	}

	if err != nil {
		return err
	}

	spans, err := r.Spans(ctx, existing.ID)
	if err != nil {
		return err
	}

	for _, span := range spans {
		if err := r.base.SpatialDelete(ctx, SpanEntity, span.ID); err != nil {
			return err
		}

		if err := r.base.Delete(ctx, SpanEntity, span.ID); err != nil {
			return err
		}
	}

	return r.base.Delete(ctx, RouteEntity, existing.ID)
}

// Get reads one route.
func (r *routes) Get(ctx context.Context, want id.ID) (asset.Route, error) {
	var row routeRow

	if err := r.base.Get(ctx, RouteEntity, want, &row); err != nil {
		return asset.Route{}, err
	}

	return row.toDomain()
}

// ForBranch finds the route carrying an electrical branch.
func (r *routes) ForBranch(ctx context.Context, branch id.ID) (asset.Route, error) {
	var rows []routeRow

	if err := r.base.Query(ctx, &rows,
		`SELECT * FROM `+RouteTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND branch_id = $1
		 LIMIT 1`, branch.String()); err != nil {
		return asset.Route{}, err
	}

	if len(rows) == 0 {
		return asset.Route{}, fmt.Errorf(
			"%w: no route carries branch %s", bserr.ErrNotFound, branch)
	}

	return rows[0].toDomain()
}

// Spans reads a route's spans in order.
//
// Geometry is rebuilt from the well-known text on the row rather than read
// back from the spatial index. The index answers "what is near here"; the row
// is the durable copy, and reading the answer from the index would make a
// missing geometry look like a missing span.
func (r *routes) Spans(ctx context.Context, route id.ID) ([]asset.Span, error) {
	if _, err := r.Get(ctx, route); err != nil {
		return nil, err
	}

	var rows []spanRow

	if err := r.base.Query(ctx, &rows,
		`SELECT * FROM `+SpanTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND route_id = $1
		 ORDER BY sequence ASC`, route.String()); err != nil {
		return nil, err
	}

	out := make([]asset.Span, 0, len(rows))

	for _, row := range rows {
		span, err := row.toDomain()
		if err != nil {
			return nil, err
		}

		out = append(out, span)
	}

	return out, nil
}

// List returns routes.
func (r *routes) List(ctx context.Context, limit int) ([]asset.Route, error) {
	if limit <= 0 {
		limit = 100
	}

	var rows []routeRow

	if err := r.base.Query(ctx, &rows,
		`SELECT * FROM `+RouteTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		 ORDER BY id DESC
		 LIMIT $1`, limit); err != nil {
		return nil, err
	}

	out := make([]asset.Route, 0, len(rows))

	for _, row := range rows {
		route, err := row.toDomain()
		if err != nil {
			return nil, err
		}

		out = append(out, route)
	}

	return out, nil
}

// toDomain maps a stored route back.
func (r routeRow) toDomain() (asset.Route, error) {
	routeID, err := gid.ParseRoute(r.ID)
	if err != nil {
		return asset.Route{}, fmt.Errorf("geo: stored route id %q: %w", r.ID, err)
	}

	branchID, err := id.Parse(r.BranchID)
	if err != nil {
		return asset.Route{}, fmt.Errorf("geo: stored branch id %q: %w", r.BranchID, err)
	}

	return asset.Route{
		ID:         routeID,
		BranchID:   branchID,
		Provenance: asset.Provenance(r.Provenance),
		LengthM:    r.LengthM,
	}, nil
}
