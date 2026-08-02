// Package store composes the datasource extension's per-domain stores.
//
// The composite is the pattern the project brief calls a "store factory". No
// such type exists in the substrate — it is this convention: one Store
// interface per domain package, a composite gathering them plus the lifecycle,
// and implementations under store/memory and store/fabriq.
package store

import (
	"github.com/xraph/blackstart/extensions/datasource/feed"
	"github.com/xraph/blackstart/extensions/datasource/source"
	"github.com/xraph/blackstart/pkg/store"
)

// Store is everything the datasource extension persists.
type Store interface {
	store.Lifecycle

	// Sources reads and writes data sources.
	Sources() source.Store

	// Feeds reads and writes the retrievable resources within them.
	Feeds() feed.Store
}
