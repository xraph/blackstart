package memory

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/xraph/blackstart/extensions/hazard/conductor"
	"github.com/xraph/blackstart/extensions/hazard/store"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

// Assignments returns the assignment store.
func (s *Store) Assignments() store.AssignmentStore { return (*assignments)(s) }

type assignments Store

func (s *assignments) Put(_ context.Context, in conductor.Assignment) error {
	if err := in.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// One assignment per branch. The Fabriq store enforces this with a unique
	// index on (tenant_id, branch_id); here it is an explicit sweep, because a
	// memory store that quietly allowed two would let every engine test pass
	// against a rule production does not have.
	for existing, got := range s.assignments {
		if got.BranchID == in.BranchID && existing != in.ID {
			delete(s.assignments, existing)
		}
	}

	s.assignments[in.ID] = in

	return nil
}

func (s *assignments) Get(_ context.Context, want id.ID) (conductor.Assignment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	found, ok := s.assignments[want]
	if !ok {
		return conductor.Assignment{}, fmt.Errorf(
			"%w: assignment %s", bserr.ErrNotFound, want)
	}

	return found, nil
}

func (s *assignments) ForBranch(
	_ context.Context, branch id.ID,
) (conductor.Assignment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, found := range s.assignments {
		if found.BranchID == branch {
			return found, nil
		}
	}

	return conductor.Assignment{}, fmt.Errorf(
		"%w: no conductor assigned to branch %s", bserr.ErrNotFound, branch)
}

func (s *assignments) List(
	_ context.Context, p pkgstore.Page,
) ([]conductor.Assignment, error) {
	p = p.Normalize()

	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]conductor.Assignment, 0, len(s.assignments))
	for _, existing := range s.assignments {
		out = append(out, existing)
	}

	slices.SortFunc(out, func(left, right conductor.Assignment) int {
		return cmp.Compare(left.ID.String(), right.ID.String())
	})

	if !p.After.IsZero() {
		after := p.After.String()
		out = slices.DeleteFunc(out, func(got conductor.Assignment) bool {
			return got.ID.String() <= after
		})
	}

	if len(out) > p.Limit {
		out = out[:p.Limit]
	}

	return out, nil
}

func (s *assignments) Delete(_ context.Context, want id.ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.assignments[want]; !ok {
		return fmt.Errorf("%w: assignment %s", bserr.ErrNotFound, want)
	}

	delete(s.assignments, want)

	return nil
}
