// Package store composes the contingency extension's per-domain stores.
//
// The composite is the pattern the project brief calls a "store factory". No
// such type exists in the substrate — it is this convention: one Store
// interface per domain package, a composite gathering them plus the lifecycle,
// and implementations under store/memory and store/fabriq.
package store

import (
	"github.com/xraph/blackstart/extensions/contingency/sweep"
	"github.com/xraph/blackstart/pkg/store"
)

// Store is everything the contingency extension persists.
type Store interface {
	store.Lifecycle

	// Runs reads and writes contingency analyses.
	Runs() sweep.Store
}
