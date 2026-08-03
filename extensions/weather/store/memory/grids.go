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
)

// Grids returns the grid store.
func (s *Store) Grids() store.GridStore { return (*grids)(s) }

type grids Store

func (s *grids) Create(_ context.Context, in forecast.Grid) error {
	if err := in.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.cycles[in.CycleID]; !ok {
		return fmt.Errorf(
			"%w: grid %s names cycle %s, which does not exist",
			bserr.ErrNotFound, in.ID, in.CycleID)
	}

	s.grids[in.ID] = in

	return nil
}

func (s *grids) Get(_ context.Context, want id.ID) (forecast.Grid, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	found, ok := s.grids[want]
	if !ok {
		return forecast.Grid{}, fmt.Errorf("%w: grid %s", bserr.ErrNotFound, want)
	}

	return found, nil
}

func (s *grids) ForCycle(_ context.Context, cycle id.ID) ([]forecast.Grid, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]forecast.Grid, 0)

	for _, existing := range s.grids {
		if existing.CycleID == cycle {
			out = append(out, existing)
		}
	}

	// Forecast hour then field, which is the order a caller walking a run
	// expects and the order the Fabriq store's index provides.
	slices.SortFunc(out, func(left, right forecast.Grid) int {
		if by := cmp.Compare(left.ForecastHour, right.ForecastHour); by != 0 {
			return by
		}

		return cmp.Compare(string(left.Field), string(right.Field))
	})

	return out, nil
}

func (s *grids) MarkSampled(
	_ context.Context, want id.ID, at time.Time, covered, total int,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	found, ok := s.grids[want]
	if !ok {
		return fmt.Errorf("%w: grid %s", bserr.ErrNotFound, want)
	}

	found.SampledAt = at
	found.Covered = covered
	found.Total = total
	s.grids[want] = found

	return nil
}
