// Package store composes the scenario extension's per-domain stores.
//
// The composite is the pattern the project brief calls a "store factory". No
// such type exists in the substrate — it is this convention, the same one
// extensions/cascade/store and extensions/contingency/store follow: one
// Store interface per domain package, a composite gathering them plus the
// lifecycle, and implementations under store/memory and, where it earns its
// keep, store/fabriq.
package store

import (
	"github.com/xraph/blackstart/extensions/scenario/scenario"
	"github.com/xraph/blackstart/pkg/store"
)

// Store is everything the scenario extension persists.
type Store interface {
	store.Lifecycle

	// Scenarios reads and writes the model/forecast/policy bindings.
	Scenarios() scenario.Store

	// Runs reads and writes their executions.
	Runs() scenario.RunStore
}
