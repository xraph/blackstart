// Package memory is the in-memory implementation of the risk store.
//
// Mirrors the hazard extension's memory store in shape, for the same reason:
// whatever computes against a ValueStore — the model pass, eventually an API
// handler — is tested against this rather than a hand-written double, so it
// has to behave like the Postgres implementation in the ways callers depend
// on: the same ordering and the same knowledge-time isolation between
// issuances.
package memory

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/xraph/blackstart/extensions/risk/store"
	"github.com/xraph/blackstart/pkg/id"
)

// seriesKey identifies one component's series at one issuance.
//
// A struct rather than a formatted string: there is no wire format or SQL
// table to keep in sync with here, so encoding the key loses nothing and
// spares every reader a parse step.
type seriesKey struct {
	component id.ID
	quantity  store.Quantity
	issuedAt  time.Time
}

// Store is the in-memory composite.
type Store struct {
	mu sync.RWMutex

	// series holds the values, keyed on component, quantity and issuance —
	// the knowledge-time axis — so that a read for one issuance can never see
	// rows written under another. Keying the map this way is what makes that
	// true by construction rather than by a filter a future edit could drop.
	series map[seriesKey][]store.ComponentValue
}

// New builds an empty store.
func New() *Store {
	return &Store{series: map[seriesKey][]store.ComponentValue{}}
}

// StoreName identifies the store in health output.
func (s *Store) StoreName() string { return "risk/memory" }

// Migrate is a no-op: there is no schema to create.
func (s *Store) Migrate(context.Context) error { return nil }

// Ping always succeeds.
func (s *Store) Ping(context.Context) error { return nil }

// Close releases nothing.
func (s *Store) Close() error { return nil }

// Values returns the value store.
func (s *Store) Values() store.ValueStore { return (*values)(s) }

type values Store

func (s *values) PutBatch(_ context.Context, in []store.ComponentValue) error {
	// Validated before anything is written, so a bad value partway through a
	// batch does not leave half of it stored.
	for index, value := range in {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("risk: value %d: %w", index, err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, value := range in {
		// .UTC() normalizes the same way store/fabriq/values.go does before
		// its own issued_at comparison: time.Time equality (and the map
		// hashing seriesKey relies on) includes the location pointer and the
		// monotonic reading, so an unnormalized key here could silently miss
		// a row written under a wall-clock-equal but not struct-equal
		// instant. compute.go passes cycle.IssuedAt as given; this is where
		// that gets normalized on the read side of this store.
		key := seriesKey{
			component: value.ComponentID, quantity: value.Quantity,
			issuedAt: value.IssuedAt.UTC(),
		}
		s.series[key] = append(s.series[key], value)
	}

	return nil
}

func (s *values) ForComponent(
	_ context.Context, component id.ID, quantities []store.Quantity, issuedAt, from, to time.Time,
) ([]store.ComponentValue, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]store.ComponentValue, 0)

	for _, quantity := range quantities {
		key := seriesKey{component: component, quantity: quantity, issuedAt: issuedAt.UTC()}

		for _, value := range s.series[key] {
			if !inWindow(value.ValidAt, from, to) {
				continue
			}

			out = append(out, value)
		}
	}

	slices.SortFunc(out, func(left, right store.ComponentValue) int {
		return left.ValidAt.Compare(right.ValidAt)
	})

	return out, nil
}

// inWindow reports whether an instant falls in the half-open [from, to).
//
// Half-open so that a caller walking a series hour by hour does not read
// every boundary twice.
func inWindow(at, from, to time.Time) bool {
	return !at.Before(from) && at.Before(to)
}
