package memory

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/xraph/blackstart/extensions/hazard/store"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

// Computations returns the computation store.
func (s *Store) Computations() store.ComputationStore { return (*computations)(s) }

type computations Store

func (s *computations) Create(_ context.Context, in store.Computation) error {
	if err := in.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.computations[in.ID]; ok {
		return fmt.Errorf("%w: computation %s", bserr.ErrConflict, in.ID)
	}

	s.computations[in.ID] = in

	return nil
}

func (s *computations) Get(_ context.Context, want id.ID) (store.Computation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	found, ok := s.computations[want]
	if !ok {
		return store.Computation{}, fmt.Errorf(
			"%w: computation %s", bserr.ErrNotFound, want)
	}

	return found, nil
}

func (s *computations) ForCycle(
	_ context.Context, cycle id.ID,
) ([]store.Computation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]store.Computation, 0)

	for _, existing := range s.computations {
		if existing.CycleID == cycle {
			out = append(out, existing)
		}
	}

	newestFirst(out)

	return out, nil
}

func (s *computations) List(
	_ context.Context, p pkgstore.Page,
) ([]store.Computation, error) {
	p = p.Normalize()

	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]store.Computation, 0, len(s.computations))
	for _, existing := range s.computations {
		out = append(out, existing)
	}

	newestFirst(out)

	if !p.After.IsZero() {
		after := p.After.String()
		out = slices.DeleteFunc(out, func(got store.Computation) bool {
			return got.ID.String() >= after
		})
	}

	if len(out) > p.Limit {
		out = out[:p.Limit]
	}

	return out, nil
}

// newestFirst orders computations the way both listings return them.
//
// By time, then by identifier to break ties. Two passes over the same cycle in
// the same second are not hypothetical — a re-computation triggered right after
// the first — and a sort that left them in map order would return a different
// answer each call.
func newestFirst(in []store.Computation) {
	slices.SortFunc(in, func(left, right store.Computation) int {
		if by := right.ComputedAt.Compare(left.ComputedAt); by != 0 {
			return by
		}

		return cmp.Compare(right.ID.String(), left.ID.String())
	})
}
