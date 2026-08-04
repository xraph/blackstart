// Package memory is the in-memory implementation of the hazard store.
//
// Not a throwaway mock. The compute engine is tested against this rather than
// against a hand-written double per test, so it has to behave like the Fabriq
// store in the ways the engine depends on: the same errors, the same
// one-assignment-per-branch rule, the same ordering, and the same half-open
// read window. A memory store that is merely convenient lets every engine test
// pass while production returns something else.
package memory

import (
	"context"
	"sync"
	"time"

	"github.com/xraph/blackstart/extensions/hazard/conductor"
	"github.com/xraph/blackstart/extensions/hazard/store"
	"github.com/xraph/blackstart/pkg/id"
)

// point is one stored number, keyed by series.
//
// Mirrors what the Timeseries port stores rather than the domain type, because
// this store exists to behave like that one.
type point struct {
	at    time.Time
	value float64
}

// Store is the in-memory composite.
type Store struct {
	mu sync.RWMutex

	assignments  map[id.ID]conductor.Assignment
	computations map[id.ID]store.Computation

	// series holds the values, keyed the way the hypertable is.
	series map[string][]point
}

// New builds an empty store.
func New() *Store {
	return &Store{
		assignments:  map[id.ID]conductor.Assignment{},
		computations: map[id.ID]store.Computation{},
		series:       map[string][]point{},
	}
}

// StoreName identifies the store in health output.
func (s *Store) StoreName() string { return "hazard/memory" }

// Migrate is a no-op: there is no schema to create.
func (s *Store) Migrate(context.Context) error { return nil }

// Ping always succeeds.
func (s *Store) Ping(context.Context) error { return nil }

// Close releases nothing.
func (s *Store) Close() error { return nil }

// inWindow reports whether an instant falls in the half-open [from, to).
//
// Half-open so that a caller walking a series hour by hour does not read every
// boundary twice.
func inWindow(at, from, to time.Time) bool {
	return !at.Before(from) && at.Before(to)
}
