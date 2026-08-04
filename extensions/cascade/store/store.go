// Package store composes the cascade extension's per-domain stores.
//
// The composite is the pattern the project brief calls a "store factory". No
// such type exists in the substrate — it is this convention, the same one
// extensions/contingency/store follows: one Store interface per domain
// package, a composite gathering them plus the lifecycle, and implementations
// under store/memory and, where it earns its keep, store/fabriq.
package store

import (
	"github.com/xraph/blackstart/extensions/cascade/run"
	"github.com/xraph/blackstart/pkg/store"
)

// Store is everything the cascade extension persists.
type Store interface {
	store.Lifecycle

	// Runs reads and writes cascade simulations.
	Runs() run.Store
}
