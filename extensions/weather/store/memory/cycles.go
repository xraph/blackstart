package memory

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/xraph/blackstart/extensions/weather/forecast"
	"github.com/xraph/blackstart/extensions/weather/store"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

// Cycles returns the cycle store.
func (s *Store) Cycles() store.CycleStore { return (*cycles)(s) }

type cycles Store

func (s *cycles) Create(_ context.Context, in forecast.Cycle) error {
	if err := in.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Two cycles of one model at one issuance is a contradiction rather than a
	// duplicate: nothing could say which run a sample came from. The Fabriq
	// store gets this from a unique index, so this one has to enforce it too
	// or an engine test would pass where production conflicts.
	for _, existing := range s.cycles {
		if existing.Model == in.Model && existing.IssuedAt.Equal(in.IssuedAt) {
			return fmt.Errorf(
				"%w: %s already has a cycle issued at %s",
				bserr.ErrConflict, in.Model,
				in.IssuedAt.UTC().Format(time.RFC3339))
		}
	}

	s.cycles[in.ID] = in

	return nil
}

func (s *cycles) Get(_ context.Context, want id.ID) (forecast.Cycle, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	found, ok := s.cycles[want]
	if !ok {
		return forecast.Cycle{}, fmt.Errorf("%w: cycle %s", bserr.ErrNotFound, want)
	}

	return found, nil
}

func (s *cycles) ForIssuance(
	_ context.Context, model forecast.Model, issuedAt time.Time,
) (forecast.Cycle, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, existing := range s.cycles {
		if existing.Model == model && existing.IssuedAt.Equal(issuedAt) {
			return existing, nil
		}
	}

	return forecast.Cycle{}, fmt.Errorf(
		"%w: no %s cycle issued at %s", bserr.ErrNotFound, model,
		issuedAt.UTC().Format(time.RFC3339))
}

func (s *cycles) SetStatus(
	_ context.Context, want id.ID, status forecast.CycleStatus,
) error {
	if !status.Valid() {
		return fmt.Errorf("%w: %q is not a cycle status", bserr.ErrInvalid, status)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	found, ok := s.cycles[want]
	if !ok {
		return fmt.Errorf("%w: cycle %s", bserr.ErrNotFound, want)
	}

	found.Status = status
	s.cycles[want] = found

	return nil
}

func (s *cycles) List(
	_ context.Context, f store.CycleFilter, p pkgstore.Page,
) ([]forecast.Cycle, error) {
	p = p.Normalize()

	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]forecast.Cycle, 0, len(s.cycles))

	for _, existing := range s.cycles {
		if f.Model != "" && existing.Model != f.Model {
			continue
		}

		if f.Status != "" && existing.Status != f.Status {
			continue
		}

		out = append(out, existing)
	}

	// Newest issuance first: the question asked of this list is almost always
	// "what is the most recent run", and a stable tiebreak on identifier keeps
	// two models issued at one instant in a fixed order.
	slices.SortFunc(out, func(left, right forecast.Cycle) int {
		if by := right.IssuedAt.Compare(left.IssuedAt); by != 0 {
			return by
		}

		return cmp.Compare(left.ID.String(), right.ID.String())
	})

	if len(out) > p.Limit {
		out = out[:p.Limit]
	}

	return out, nil
}
