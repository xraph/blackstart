// Package store composes the topology extension's per-domain stores.
package store

import (
	"github.com/xraph/blackstart/extensions/topology/network"
	"github.com/xraph/blackstart/pkg/store"
)

// Store is everything the topology extension persists.
type Store interface {
	store.Lifecycle

	// Network reads and writes the node-breaker model.
	Network() network.Store
}
