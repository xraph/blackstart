// Package store composes the geo extension's per-domain stores.
package store

import (
	"context"

	"github.com/xraph/blackstart/extensions/geo/asset"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/store"
)

// Store is everything the geo extension persists.
type Store interface {
	store.Lifecycle

	// Routes reads and writes carrier geometry.
	Routes() RouteStore

	// Circuits reads and writes the lines somebody actually built.
	Circuits() CircuitStore
}

// CircuitStore holds circuits imported from public sources.
//
// No Put. A circuit is never created from nothing: it always comes from a
// source that already identified it, and the only way in is Reconcile, which
// keys on that identity. A bare Put would let an import create a second
// circuit for a line it had already seen, and the second one would look
// exactly as real as the first.
type CircuitStore interface {
	// Reconcile stores a circuit, or updates the one already carrying the same
	// Source and ExternalID and returns it with its existing identifier.
	//
	// This is what makes an import re-runnable. The identifier the caller
	// supplies is used only if the pair is new — re-importing an OSM way keeps
	// the identifier the route already points at, so the geometry follows the
	// circuit rather than being orphaned by a fresh one.
	Reconcile(ctx context.Context, circuit asset.Circuit) (asset.Circuit, error)

	// Get reads one circuit.
	Get(ctx context.Context, want id.ID) (asset.Circuit, error)

	// ByExternal finds a circuit by the key its source used.
	ByExternal(ctx context.Context, source, external string) (asset.Circuit, error)

	// List returns circuits.
	List(ctx context.Context, limit int) ([]asset.Circuit, error)
}

// RouteStore holds routes and their spans.
//
// Spans are written with their route rather than separately, because a route
// without its spans has a length that nothing adds up to — and the PostGIS
// write is a lifecycle hook on the route's own transaction, so they arrive
// together or not at all.
type RouteStore interface {
	// Put writes a route and replaces its spans.
	//
	// Replaces rather than appends: a re-survey produces a new set of spans
	// for the same route, and appending would leave the old path in place
	// alongside the new one with nothing to say which is current.
	Put(ctx context.Context, route asset.Route, spans []asset.Span) error

	// Get reads one route.
	Get(ctx context.Context, want id.ID) (asset.Route, error)

	// ForCarrier finds the route carrying a branch or a circuit.
	ForCarrier(ctx context.Context, carrier id.ID) (asset.Route, error)

	// Spans reads a route's spans in order.
	Spans(ctx context.Context, route id.ID) ([]asset.Span, error)

	// List returns routes.
	List(ctx context.Context, limit int) ([]asset.Route, error)
}
