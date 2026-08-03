// Package memory is the in-memory implementation of the weather store.
//
// Not a throwaway mock. The engine is tested against this rather than against
// a hand-written double per test, so it has to behave like the Fabriq store in
// the ways the engine depends on: the same errors, the same uniqueness rules,
// the same ordering, and the same half-open read window. A memory store that
// is merely convenient lets every engine test pass while production returns
// something else.
package memory

import (
	"context"
	"sync"
	"time"

	"github.com/xraph/blackstart/extensions/weather/forecast"
	"github.com/xraph/blackstart/extensions/weather/observation"
	"github.com/xraph/blackstart/pkg/id"
)

// point is one stored number, keyed by series.
//
// Mirrors query.Point rather than the domain type, because that is what the
// Timeseries port stores and this store exists to behave like that one.
type point struct {
	at    time.Time
	value float64
}

// Store is the in-memory composite.
type Store struct {
	mu sync.RWMutex

	cycles   map[id.ID]forecast.Cycle
	grids    map[id.ID]forecast.Grid
	stations map[id.ID]observation.Station

	// series holds both planes, keyed the same way the hypertables are. Two
	// maps rather than one, matching the two tables — an observation key has
	// two parts and a forecast key three, so a single map would work but would
	// hide the fact that they are separate series.
	forecastSeries map[string][]point
	stationSeries  map[string][]point
}

// New builds an empty store.
func New() *Store {
	return &Store{
		cycles:         map[id.ID]forecast.Cycle{},
		grids:          map[id.ID]forecast.Grid{},
		stations:       map[id.ID]observation.Station{},
		forecastSeries: map[string][]point{},
		stationSeries:  map[string][]point{},
	}
}

// StoreName identifies the store in health output.
func (s *Store) StoreName() string { return "weather/memory" }

// Migrate is a no-op: there is no schema to create.
func (s *Store) Migrate(context.Context) error { return nil }

// Ping always succeeds.
func (s *Store) Ping(context.Context) error { return nil }

// Close releases nothing.
func (s *Store) Close() error { return nil }
