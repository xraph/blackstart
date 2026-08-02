// Package memory is the in-memory implementation of the geo store.
package memory

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/xraph/blackstart/extensions/geo/asset"
	"github.com/xraph/blackstart/extensions/geo/store"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Store is the in-memory composite.
type Store struct {
	mu sync.RWMutex

	routes map[id.ID]asset.Route
	spans  map[id.ID][]asset.Span
}

// New builds an empty store.
func New() *Store {
	return &Store{
		routes: map[id.ID]asset.Route{},
		spans:  map[id.ID][]asset.Span{},
	}
}

// Migrate is a no-op: there is no schema to create.
func (s *Store) Migrate(context.Context) error { return nil }

// Ping always succeeds.
func (s *Store) Ping(context.Context) error { return nil }

// Close releases nothing.
func (s *Store) Close() error { return nil }

// Routes returns the route store.
func (s *Store) Routes() store.RouteStore { return (*routes)(s) }

type routes Store

func (s *routes) Put(_ context.Context, route asset.Route, spans []asset.Span) error {
	if err := route.Validate(); err != nil {
		return err
	}

	for index, span := range spans {
		if err := span.Validate(); err != nil {
			return fmt.Errorf("span %d: %w", index, err)
		}

		if span.RouteID != route.ID {
			return fmt.Errorf(
				"%w: span %d belongs to route %s, not %s",
				bserr.ErrInvalid, index, span.RouteID, route.ID)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.routes[route.ID] = route

	// Replaced, not appended. A re-survey produces a new set of spans for the
	// same route, and appending would leave the old path alongside the new one
	// with nothing to say which is current.
	s.spans[route.ID] = slices.Clone(spans)

	return nil
}

func (s *routes) Get(_ context.Context, want id.ID) (asset.Route, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	found, ok := s.routes[want]
	if !ok {
		return asset.Route{}, fmt.Errorf("%w: route %s", bserr.ErrNotFound, want)
	}

	return found, nil
}

func (s *routes) ForBranch(_ context.Context, branch id.ID) (asset.Route, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, route := range s.routes {
		if route.BranchID == branch {
			return route, nil
		}
	}

	return asset.Route{}, fmt.Errorf(
		"%w: no route carries branch %s", bserr.ErrNotFound, branch)
}

func (s *routes) Spans(_ context.Context, route id.ID) ([]asset.Span, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.routes[route]; !ok {
		return nil, fmt.Errorf("%w: route %s", bserr.ErrNotFound, route)
	}

	out := slices.Clone(s.spans[route])

	// Ordered along the route from its first end, because a span's position is
	// what relates it to its neighbors and to the weather at that point.
	slices.SortFunc(out, func(left, right asset.Span) int {
		return cmp.Compare(left.Sequence, right.Sequence)
	})

	return out, nil
}

func (s *routes) List(_ context.Context, limit int) ([]asset.Route, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := slices.Collect(maps.Values(s.routes))

	slices.SortFunc(out, func(left, right asset.Route) int {
		return cmp.Compare(left.ID.String(), right.ID.String())
	})

	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}

	return out, nil
}
