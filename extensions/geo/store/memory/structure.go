package memory

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/xraph/blackstart/extensions/geo/asset"
	"github.com/xraph/blackstart/extensions/geo/store"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Structures returns the structure store.
func (s *Store) Structures() store.StructureStore { return (*structures)(s) }

type structures Store

func (s *structures) Put(_ context.Context, route id.ID, in []asset.Structure) error {
	for index, structure := range in {
		if err := structure.Validate(); err != nil {
			return fmt.Errorf("structure %d: %w", index, err)
		}

		if structure.RouteID != route {
			return fmt.Errorf(
				"%w: structure %d names route %s, not %s",
				bserr.ErrInvalid, index, structure.RouteID, route)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Replaced, not appended, for RouteStore.Put's reason: a re-survey
	// produces a new set of structures for the same route, and appending
	// would leave the old positions beside the new ones with nothing to say
	// which is current.
	for _, old := range s.byRoute[route] {
		delete(s.structures, old)
	}

	sorted := slices.Clone(in)
	slices.SortFunc(sorted, func(left, right asset.Structure) int {
		return cmp.Compare(left.Ordinal, right.Ordinal)
	})

	ids := make([]id.ID, 0, len(sorted))

	for _, structure := range sorted {
		s.structures[structure.ID] = structure
		ids = append(ids, structure.ID)
	}

	s.byRoute[route] = ids

	return nil
}

// ForRoute returns a route's structures in ordinal order.
//
// No sort here: Put stores them sorted by Ordinal, so a caller cannot receive
// them scattered.
func (s *structures) ForRoute(_ context.Context, route id.ID) ([]asset.Structure, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := s.byRoute[route]
	out := make([]asset.Structure, 0, len(ids))

	for _, want := range ids {
		out = append(out, s.structures[want])
	}

	return out, nil
}

func (s *structures) Get(_ context.Context, want id.ID) (asset.Structure, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	found, ok := s.structures[want]
	if !ok {
		return asset.Structure{}, fmt.Errorf("%w: structure %s", bserr.ErrNotFound, want)
	}

	return found, nil
}
