package memory

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/xraph/blackstart/extensions/weather/observation"
	"github.com/xraph/blackstart/extensions/weather/store"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

// Stations returns the station store.
func (s *Store) Stations() store.StationStore { return (*stations)(s) }

type stations Store

func (s *stations) Put(_ context.Context, in observation.Station) error {
	if err := in.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.stations[in.ID] = in

	return nil
}

func (s *stations) Get(_ context.Context, want id.ID) (observation.Station, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	found, ok := s.stations[want]
	if !ok {
		return observation.Station{}, fmt.Errorf(
			"%w: station %s", bserr.ErrNotFound, want)
	}

	return found, nil
}

func (s *stations) ByCode(_ context.Context, code string) (observation.Station, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, existing := range s.stations {
		if existing.Code == code {
			return existing, nil
		}
	}

	return observation.Station{}, fmt.Errorf(
		"%w: no station with code %q", bserr.ErrNotFound, code)
}

func (s *stations) List(
	_ context.Context, p pkgstore.Page,
) ([]observation.Station, error) {
	p = p.Normalize()

	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]observation.Station, 0, len(s.stations))
	for _, existing := range s.stations {
		out = append(out, existing)
	}

	slices.SortFunc(out, func(left, right observation.Station) int {
		return cmp.Compare(left.ID.String(), right.ID.String())
	})

	if len(out) > p.Limit {
		out = out[:p.Limit]
	}

	return out, nil
}
