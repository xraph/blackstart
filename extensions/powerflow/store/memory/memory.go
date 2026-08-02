// Package memory is the in-memory implementation of the powerflow store.
//
// Not a throwaway mock. The engine is tested against this rather than against
// a hand-written double per test, so it has to behave like the real store in
// the ways the engine depends on: the same errors, the same append-only
// semantics, and the same ordering. A memory store that ordered differently
// would let every engine test pass while the real store returned a different
// list.
package memory

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/xraph/blackstart/extensions/powerflow/solution"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Store is the in-memory composite.
type Store struct {
	mu sync.RWMutex

	solutions map[id.ID]solution.Solution

	// order preserves insertion sequence, so ListByModel can return most
	// recent first without depending on map iteration — which Go deliberately
	// randomizes, and which would make every list a different list.
	order []id.ID
}

// New builds an empty store.
func New() *Store {
	return &Store{solutions: map[id.ID]solution.Solution{}}
}

// Migrate is a no-op: there is no schema to create.
func (s *Store) Migrate(context.Context) error { return nil }

// Ping always succeeds. An in-memory store is reachable whenever the process
// is, so reporting anything else would be inventing a failure mode.
func (s *Store) Ping(context.Context) error { return nil }

// Close releases nothing.
func (s *Store) Close() error { return nil }

// Solutions returns the solution store.
func (s *Store) Solutions() solution.Store { return (*solutions)(s) }

type solutions Store

// Create records a solution.
//
// Rejects a duplicate identifier rather than overwriting. Solutions are
// append-only, and an overwrite would replace a number someone may already
// have cited with a different one under the same name.
func (s *solutions) Create(_ context.Context, in solution.Solution) (solution.Solution, error) {
	if err := in.Validate(); err != nil {
		return solution.Solution{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.solutions[in.ID]; exists {
		return solution.Solution{}, fmt.Errorf(
			"%w: solution %s already exists; solutions are append-only",
			bserr.ErrConflict, in.ID)
	}

	s.solutions[in.ID] = in
	s.order = append(s.order, in.ID)

	return in, nil
}

// Get retrieves one solution.
func (s *solutions) Get(_ context.Context, solutionID id.ID) (solution.Solution, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	found, ok := s.solutions[solutionID]
	if !ok {
		return solution.Solution{}, fmt.Errorf("%w: solution %s", bserr.ErrNotFound, solutionID)
	}

	return found, nil
}

// ListByModel returns solutions for one network, most recent first.
//
// A limit of zero or less returns everything, which is what a CLI wants; an
// HTTP handler passes a real bound.
func (s *solutions) ListByModel(
	_ context.Context, modelHash string, limit int,
) ([]solution.Solution, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []solution.Solution

	// Walked in insertion order rather than over the map, then reversed. Go
	// randomizes map iteration on purpose, and a list that came back in a
	// different order each call would make every downstream golden noise.
	for _, key := range s.order {
		if found := s.solutions[key]; found.ModelHash == modelHash {
			out = append(out, found)
		}
	}

	slices.Reverse(out)

	// Ties on SolvedAt break on identifier, which is chronologically sortable,
	// so two solutions recorded in the same instant still order reproducibly.
	slices.SortStableFunc(out, func(left, right solution.Solution) int {
		if by := right.SolvedAt.Compare(left.SolvedAt); by != 0 {
			return by
		}

		return cmp.Compare(right.ID.String(), left.ID.String())
	})

	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}

	return out, nil
}
