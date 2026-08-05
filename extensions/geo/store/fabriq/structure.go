package fabriq

import (
	"context"
	"fmt"

	"github.com/xraph/blackstart/extensions/geo/asset"
	"github.com/xraph/blackstart/extensions/geo/shape"
	"github.com/xraph/blackstart/extensions/geo/store"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Structures returns the structure store.
func (s *Store) Structures() store.StructureStore { return (*structures)(s) }

type structures Store

// Put replaces every structure on a route.
//
// Follows routeStore.Put: the rows go through Fabriq's command path and each
// structure's point goes through its spatial port as a direct write outside
// the aggregate's own transaction. The order is the same for the same
// reason — row first, geometry second — so a crash between them leaves a
// structure whose geometry is missing from the index but present as
// well-known text on its own row, which `bsctl geo reindex` can replay. The
// reverse order would leave indexed geometry for a structure that does not
// exist.
func (s *structures) Put(ctx context.Context, route id.ID, in []asset.Structure) error {
	for index, structure := range in {
		if err := structure.Validate(); err != nil {
			return fmt.Errorf("geo: structure %d: %w", index, err)
		}

		if structure.RouteID != route {
			return fmt.Errorf(
				"%w: structure %d belongs to route %s, not %s",
				bserr.ErrInvalid, index, structure.RouteID, route)
		}
	}

	// Replaced, not appended, for RouteStore.Put's reason: a re-import
	// produces a new set of structures for the same route, and appending
	// would leave the old positions beside the new ones with nothing to say
	// which is current. An empty set is a legitimate replacement too — it
	// empties the route rather than being treated as "nothing to do".
	if err := s.forget(ctx, route); err != nil {
		return err
	}

	for index, structure := range in {
		wkt := structure.Location.EWKT()

		if err := s.base.Create(ctx, StructureEntity, structure.ID, structureRow{
			ID:       structure.ID.String(),
			RouteID:  structure.RouteID.String(),
			Ordinal:  structure.Ordinal,
			GeomWKT:  wkt,
			HeightM:  structure.HeightM,
			Kind:     structure.Kind,
			Material: structure.Material,
		}); err != nil {
			return fmt.Errorf("geo: structure %d: %w", index, err)
		}

		// The meta carries what a spatial query filters on without joining
		// back, for the reason SpanEntity's write does.
		if err := s.base.Spatial(ctx, StructureEntity, structure.ID, wkt, shape.SRID,
			map[string]any{"route_id": structure.RouteID.String()}); err != nil {
			return err
		}
	}

	return nil
}

// forget removes a route's existing structures and their geometry.
//
// Geometry is deleted before the rows that name it, for routes.forget's
// reason: a crash in between leaves an indexed geometry for a structure that
// still exists, which the next write replaces; the other order would leave
// geometry pointing at nothing, and nothing would ever visit it again to
// clean it up.
func (s *structures) forget(ctx context.Context, route id.ID) error {
	existing, err := s.ForRoute(ctx, route)
	if err != nil {
		return err
	}

	for _, structure := range existing {
		if err := s.base.SpatialDelete(ctx, StructureEntity, structure.ID); err != nil {
			return err
		}

		if err := s.base.Delete(ctx, StructureEntity, structure.ID); err != nil {
			return err
		}
	}

	return nil
}

// ForRoute returns a route's structures in ordinal order.
func (s *structures) ForRoute(ctx context.Context, route id.ID) ([]asset.Structure, error) {
	var rows []structureRow

	if err := s.base.Query(ctx, &rows,
		`SELECT * FROM `+StructureTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND route_id = $1
		 ORDER BY ordinal ASC`, route.String()); err != nil {
		return nil, err
	}

	out := make([]asset.Structure, 0, len(rows))

	for _, row := range rows {
		structure, err := row.toDomain()
		if err != nil {
			return nil, err
		}

		out = append(out, structure)
	}

	return out, nil
}

// Get reads one structure.
func (s *structures) Get(ctx context.Context, want id.ID) (asset.Structure, error) {
	var row structureRow

	if err := s.base.Get(ctx, StructureEntity, want, &row); err != nil {
		return asset.Structure{}, err
	}

	return row.toDomain()
}
