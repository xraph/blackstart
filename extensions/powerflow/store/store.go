// Package store composes the powerflow extension's per-domain stores.
//
// The composite is the pattern the project brief calls a "store factory". No
// such type exists in the substrate — it is this convention: one Store
// interface per domain package, a composite gathering them plus the lifecycle,
// and implementations under store/memory and store/fabriq.
package store

import (
	"github.com/xraph/blackstart/extensions/powerflow/solution"
	"github.com/xraph/blackstart/pkg/store"
)

// Store is everything the powerflow extension persists.
type Store interface {
	store.Lifecycle

	// Solutions reads and writes solved power flows.
	Solutions() solution.Store
}
