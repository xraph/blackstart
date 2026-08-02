// Package memory is the in-memory implementation of the datasource store.
//
// It is not a throwaway mock. Services are tested against this rather than
// against a hand-written double per test, so it has to behave like the real
// store in the ways the services depend on: the same errors, the same
// uniqueness rules, and the same bitemporal semantics.
//
// That last one is the reason it is more than a map. A memory store that
// ignores the temporal context would let every service test pass while the
// real store returned different rows, which is precisely the divergence
// pkg/bitemporal exists to prevent.
package memory

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/xraph/blackstart/extensions/datasource/feed"
	"github.com/xraph/blackstart/extensions/datasource/source"
	"github.com/xraph/blackstart/pkg/bitemporal"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/store"
)

// version pairs a value with its position on both time axes.
type version[T any] struct {
	value  T
	record bitemporal.Record
}

// Store is the in-memory composite.
type Store struct {
	mu sync.RWMutex

	sources map[id.ID][]version[source.Source]
	feeds   map[id.ID][]version[feed.Feed]

	// now is injectable so a test can advance knowledge time deliberately
	// rather than sleeping.
	now func() bitemporal.Instant
}

// New builds an empty store.
func New() *Store {
	return &Store{
		sources: make(map[id.ID][]version[source.Source]),
		feeds:   make(map[id.ID][]version[feed.Feed]),
		now:     bitemporal.Now,
	}
}

// SetClock replaces the knowledge-time source. Tests use it to make a
// correction land at a chosen instant; production never calls it.
func (s *Store) SetClock(now func() bitemporal.Instant) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.now = now
}

// StoreName identifies the store in health output.
func (s *Store) StoreName() string { return "datasource/memory" }

// Migrate is a no-op: there is no schema to bring up to date.
func (s *Store) Migrate(context.Context) error { return nil }

// Ping always succeeds.
func (s *Store) Ping(context.Context) error { return nil }

// Close drops everything.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sources = make(map[id.ID][]version[source.Source])
	s.feeds = make(map[id.ID][]version[feed.Feed])

	return nil
}

// Sources returns the source store.
func (s *Store) Sources() source.Store { return (*sourceStore)(s) }

// Feeds returns the feed store.
func (s *Store) Feeds() feed.Store { return (*feedStore)(s) }

// resolve returns the single version answering a read, mirroring the SQL
// predicate: both axes must contain their instant.
func resolve[T any](versions []version[T], asOf bitemporal.AsOf) (T, bool) {
	for _, v := range versions {
		if v.record.Matches(asOf) {
			return v.value, true
		}
	}

	var zero T

	return zero, false
}

// current returns the version that is presently believed, which is the one a
// write supersedes.
func current[T any](versions []version[T]) (version[T], int, bool) {
	for i, v := range versions {
		if v.record.IsCurrent() {
			return v, i, true
		}
	}

	return version[T]{}, -1, false
}

// paginate applies keyset pagination over identifiers, which sort
// chronologically.
func paginate[T any](items []T, keys []id.ID, p store.Page) []T {
	p = p.Normalize()

	start := 0

	if !p.IsFirst() {
		after := p.After.String()

		for i, k := range keys {
			if k.String() > after {
				start = i

				break
			}

			start = i + 1
		}
	}

	if start >= len(items) {
		return nil
	}

	end := min(start+p.Limit, len(items))

	return slices.Clone(items[start:end])
}

// errNoCurrentVersion means an update was attempted against a row with no
// currently-believed version, which should be impossible.
var errNoCurrentVersion = errors.New("datasource/memory: no current version")
