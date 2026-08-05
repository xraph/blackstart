// Package fabriq is the Fabriq-backed implementation of the risk store.
//
// The value row is unlike hazard's and weather's series tables in the one way
// that matters: it carries row-level security. Those two write through
// Fabriq's Timeseries port, whose fixed six-column shape (time, tenant_id,
// tag_id, value, quality, scope_id) has no room for a probability and a
// scalar as two separate numbers, and whose columnstore forbids RLS outright.
// This table needs both numbers, so it is written as an ordinary Fabriq
// aggregate — one Create per row, going through the command plane and the
// outbox like every other entity in this codebase — and made a hypertable
// only as a scale optimization on top of that, not as a replacement for it.
// A regular (uncompressed) hypertable has no objection to RLS, which is what
// makes the combination possible here and not there.
//
// Models are unexported and per-backend, declared beside the store that uses
// them, with explicit mappers to and from the domain types. The domain
// structs carry no grove tags: keeping the persistence shape separate is what
// lets the column layout change without touching the type the rest of the
// platform passes around.
package fabriq

import (
	"context"
	"fmt"

	"github.com/xraph/fabriq"

	"github.com/xraph/blackstart/extensions/risk/store"
	"github.com/xraph/blackstart/pkg/store/fabriqstore"
)

// ValueSeries holds the computed probabilities.
//
// Not "blackstart_risk_values": every table in this codebase is singular,
// and pluralizing this one would make it the exception for no reason.
const ValueSeries = "blackstart_risk_value"

// ValueEntity is the registry name for one value row.
const ValueEntity = "risk_value"

// Store is the Fabriq-backed composite.
type Store struct {
	base *fabriqstore.Base
}

// New builds the store over a Fabriq facade.
func New(fab *fabriq.Fabriq, tenant string) (*Store, error) {
	base, err := fabriqstore.New(fab, tenant)
	if err != nil {
		return nil, fmt.Errorf("risk: building the store: %w", err)
	}

	return &Store{base: base}, nil
}

// StoreName identifies the store in health output and migration logs.
func (s *Store) StoreName() string { return "risk/fabriq" }

// Migrate is a no-op here. Schema changes are a discrete deploy step run
// through the migration group in migrations.go, never something a store does
// on the way up — a migration racing N replicas starting is a bad way to
// learn why Fabriq's documentation insists on the separation.
func (s *Store) Migrate(context.Context) error { return nil }

// Ping reports whether the source of truth is reachable.
func (s *Store) Ping(ctx context.Context) error { return s.base.Ping(ctx) }

// Close releases nothing: the database handle is borrowed from the host, and
// closing another component's connection breaks every other extension.
func (s *Store) Close() error { return nil }

// Values returns the value store.
func (s *Store) Values() store.ValueStore { return &valueStore{base: s.base} }
