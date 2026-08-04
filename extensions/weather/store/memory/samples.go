package memory

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/xraph/blackstart/extensions/weather/forecast"
	"github.com/xraph/blackstart/extensions/weather/observation"
	"github.com/xraph/blackstart/extensions/weather/store"
)

// Samples returns the sample store.
func (s *Store) Samples() store.SampleStore { return (*samples)(s) }

type samples Store

func (s *samples) WriteForecast(_ context.Context, in []forecast.Sample) error {
	// Validated before anything is written, so a bad sample partway through a
	// batch does not leave half of it stored. The Timeseries port's BulkWrite
	// is one multi-row INSERT and therefore atomic per call; this matches it.
	for index, sample := range in {
		if err := sample.Validate(); err != nil {
			return fmt.Errorf("weather: sample %d: %w", index, err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, sample := range in {
		key := forecast.SampleKey(sample.SpanID, sample.Field, sample.IssuedAt)
		s.forecastSeries[key] = append(
			s.forecastSeries[key], point{at: sample.ValidAt, value: sample.Value})
	}

	return nil
}

func (s *samples) ReadForecast(
	_ context.Context, keys []string, from, to time.Time,
) ([]forecast.Sample, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]forecast.Sample, 0)

	for _, key := range keys {
		spanID, got, issuedAt, err := forecast.ParseSampleKey(key)
		if err != nil {
			return nil, err
		}

		for _, p := range s.forecastSeries[key] {
			if !inWindow(p.at, from, to) {
				continue
			}

			out = append(out, forecast.Sample{
				SpanID:   spanID,
				Field:    got,
				IssuedAt: issuedAt,
				ValidAt:  p.at,
				Value:    p.value,
			})
		}
	}

	slices.SortFunc(out, func(left, right forecast.Sample) int {
		return left.ValidAt.Compare(right.ValidAt)
	})

	return out, nil
}

func (s *samples) WriteObservations(
	_ context.Context, in []observation.Observation,
) error {
	for index, obs := range in {
		if err := obs.Validate(); err != nil {
			return fmt.Errorf("weather: observation %d: %w", index, err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, obs := range in {
		key := observation.SampleKey(obs.StationID, obs.Field)
		s.stationSeries[key] = append(
			s.stationSeries[key], point{at: obs.ObservedAt, value: obs.Value})
	}

	return nil
}

func (s *samples) ReadObservations(
	_ context.Context, keys []string, from, to time.Time,
) ([]observation.Observation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]observation.Observation, 0)

	for _, key := range keys {
		stationID, got, err := observation.ParseSampleKey(key)
		if err != nil {
			return nil, err
		}

		for _, p := range s.stationSeries[key] {
			if !inWindow(p.at, from, to) {
				continue
			}

			out = append(out, observation.Observation{
				StationID:  stationID,
				Field:      got,
				ObservedAt: p.at,
				Value:      p.value,
			})
		}
	}

	slices.SortFunc(out, func(left, right observation.Observation) int {
		return left.ObservedAt.Compare(right.ObservedAt)
	})

	return out, nil
}

// inWindow reports whether an instant falls in the half-open [from, to).
//
// Half-open so that a caller walking a series hour by hour does not read every
// boundary twice.
func inWindow(at, from, to time.Time) bool {
	return !at.Before(from) && at.Before(to)
}
