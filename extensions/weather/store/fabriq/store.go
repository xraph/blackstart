// Package fabriq is the Fabriq-backed implementation of the weather store.
//
// Models are unexported and per-backend, each declared beside the store that
// uses it, with explicit mappers to and from the domain types. The domain
// structs carry no grove tags: keeping the persistence shape separate is what
// lets the column layout change without touching the type the rest of the
// platform passes around.
//
// Every relational write goes through fabriqstore.Base, which stamps the
// tenant, wraps the command and translates the error. The sample store is the
// one exception and says so at length — it writes through fabriq's Timeseries
// port, which bypasses the outbox by design.
package fabriq

import (
	"context"
	"fmt"

	"github.com/xraph/fabriq"

	"github.com/xraph/blackstart/extensions/weather/store"
	"github.com/xraph/blackstart/pkg/store/fabriqstore"
)

// Table names, prefixed with the extension so a shared schema stays legible.
const (
	CycleTable   = "blackstart_weather_cycle"
	GridTable    = "blackstart_weather_grid"
	StationTable = "blackstart_weather_station"
)

// Series table names.
//
// Not entities and not in the registry: these are Timescale hypertables that
// fabriq's Timeseries port writes by name, and they carry no id or version
// column because the port's INSERT names exactly six columns and no others.
const (
	// ForecastSeries holds per-span samples, keyed span|field|issuance.
	ForecastSeries = "blackstart_weather_span_sample"

	// ObservationSeries holds station readings, keyed station|field.
	ObservationSeries = "blackstart_weather_station_obs"
)

// Entity names, as they appear in the registry and in every command.
const (
	CycleEntity   = "weather_cycle"
	GridEntity    = "weather_grid"
	StationEntity = "weather_station"
)

// Store is the Fabriq-backed composite.
type Store struct {
	base *fabriqstore.Base
}

// New builds the store over a Fabriq facade.
func New(fab *fabriq.Fabriq, tenant string) (*Store, error) {
	base, err := fabriqstore.New(fab, tenant)
	if err != nil {
		return nil, fmt.Errorf("weather: building the store: %w", err)
	}

	return &Store{base: base}, nil
}

// StoreName identifies the store in health output and migration logs.
func (s *Store) StoreName() string { return "weather/fabriq" }

// Migrate is a no-op here. Schema changes are a discrete deploy step run
// through the migration group in migrations.go, never something a store does
// on the way up — a migration racing N replicas starting is a bad way to learn
// why Fabriq's documentation insists on the separation.
func (s *Store) Migrate(context.Context) error { return nil }

// Ping reports whether the source of truth is reachable.
func (s *Store) Ping(ctx context.Context) error { return s.base.Ping(ctx) }

// Close releases nothing: the database handle is borrowed from the host, and
// closing another component's connection breaks every other extension.
func (s *Store) Close() error { return nil }

// Cycles returns the cycle store.
func (s *Store) Cycles() store.CycleStore { return &cycleStore{base: s.base} }

// Grids returns the grid store.
func (s *Store) Grids() store.GridStore { return &gridStore{base: s.base} }

// Stations returns the station store.
func (s *Store) Stations() store.StationStore { return &stationStore{base: s.base} }

// Samples returns the sample store.
func (s *Store) Samples() store.SampleStore { return &sampleStore{base: s.base} }
