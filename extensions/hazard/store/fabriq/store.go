// Package fabriq is the Fabriq-backed implementation of the hazard store.
//
// Models are unexported and per-backend, each declared beside the store that
// uses it, with explicit mappers to and from the domain types. The domain
// structs carry no grove tags: keeping the persistence shape separate is what
// lets the column layout change without touching the type the rest of the
// platform passes around.
//
// Every relational write goes through fabriqstore.Base, which stamps the
// tenant, wraps the command and translates the error. The value store is the
// one exception and says so at length — it writes through fabriq's Timeseries
// port, which bypasses the outbox by design.
package fabriq

import (
	"context"
	"fmt"

	"github.com/xraph/fabriq"

	"github.com/xraph/blackstart/extensions/hazard/store"
	"github.com/xraph/blackstart/pkg/store/fabriqstore"
)

// Table names, prefixed with the extension so a shared schema stays legible.
const (
	AssignmentTable  = "blackstart_hazard_assignment"
	ComputationTable = "blackstart_hazard_computation"
)

// ValueSeries holds the derived numbers, keyed span|quantity|issuance.
//
// Not an entity and not in the registry: this is a Timescale hypertable that
// fabriq's Timeseries port writes by name, and it carries no id or version
// column because the port's INSERT names exactly six columns and no others.
const ValueSeries = "blackstart_hazard_span_value"

// Entity names, as they appear in the registry and in every command.
const (
	AssignmentEntity  = "hazard_assignment"
	ComputationEntity = "hazard_computation"
)

// Store is the Fabriq-backed composite.
type Store struct {
	base *fabriqstore.Base
}

// New builds the store over a Fabriq facade.
func New(fab *fabriq.Fabriq, tenant string) (*Store, error) {
	base, err := fabriqstore.New(fab, tenant)
	if err != nil {
		return nil, fmt.Errorf("hazard: building the store: %w", err)
	}

	return &Store{base: base}, nil
}

// StoreName identifies the store in health output and migration logs.
func (s *Store) StoreName() string { return "hazard/fabriq" }

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

// Assignments returns the assignment store.
func (s *Store) Assignments() store.AssignmentStore { return &assignmentStore{base: s.base} }

// Computations returns the computation store.
func (s *Store) Computations() store.ComputationStore {
	return &computationStore{base: s.base}
}

// Values returns the value store.
func (s *Store) Values() store.ValueStore { return &valueStore{base: s.base} }
