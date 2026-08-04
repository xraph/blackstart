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
