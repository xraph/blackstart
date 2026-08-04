package fabriq

import (
	"context"
	"fmt"

	"github.com/xraph/fabriq"

	"github.com/xraph/blackstart/extensions/provenance/record"
	"github.com/xraph/blackstart/extensions/provenance/store"
	"github.com/xraph/blackstart/pkg/store/fabriqstore"
)

// Compile-time proof that this satisfies the composite the extension holds. A
// missing method should fail the build here rather than at the wiring in
// internal/server, where the reader has no idea which store is at fault.
var _ store.Store = (*Store)(nil)

// Store is the Fabriq-backed composite.
//
// Every write goes through fabriqstore.Base, which stamps the tenant, wraps
// the command, and translates the error. Nothing here reaches Fabriq directly,
// so a breaking change in a v0.0.x API is a one-package fix rather than a
// sweep through every store in the platform.
type Store struct {
	base *fabriqstore.Base
}

// New builds the store over a Fabriq facade.
func New(fab *fabriq.Fabriq, tenant string) (*Store, error) {
	base, err := fabriqstore.New(fab, tenant)
	if err != nil {
		return nil, fmt.Errorf("provenance: building the store: %w", err)
	}

	return &Store{base: base}, nil
}

// StoreName identifies the store in health output and migration logs.
func (s *Store) StoreName() string { return "provenance/fabriq" }

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

// Records returns the record store.
func (s *Store) Records() record.Store { return &records{base: s.base} }
