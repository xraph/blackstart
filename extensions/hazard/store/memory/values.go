package memory

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/xraph/blackstart/extensions/hazard/store"
)

// Values returns the value store.
func (s *Store) Values() store.ValueStore { return (*values)(s) }

type values Store

func (s *values) Write(_ context.Context, in []store.SpanValue) error {
	// Validated before anything is written, so a bad value partway through a
	// batch does not leave half of it stored. The Timeseries port's BulkWrite
	// is one multi-row INSERT and therefore atomic per call; this matches it.
	for index, value := range in {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("hazard: value %d: %w", index, err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, value := range in {
		key := value.Key()
		s.series[key] = append(s.series[key], point{at: value.ValidAt, value: value.Value})
	}

	return nil
}

func (s *values) Read(
	_ context.Context, keys []string, from, to time.Time,
) ([]store.SpanValue, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]store.SpanValue, 0)

	for _, key := range keys {
		spanID, quantity, issuedAt, err := store.ParseValueKey(key)
		if err != nil {
			return nil, err
		}

		for _, p := range s.series[key] {
			if !inWindow(p.at, from, to) {
				continue
			}

			out = append(out, store.SpanValue{
				SpanID:   spanID,
				Quantity: quantity,
				IssuedAt: issuedAt,
				ValidAt:  p.at,
				Value:    p.value,
			})
		}
	}

	slices.SortFunc(out, func(left, right store.SpanValue) int {
		return left.ValidAt.Compare(right.ValidAt)
	})

	return out, nil
}
