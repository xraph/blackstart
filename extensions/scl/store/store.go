// Package store composes the SCL extension's per-domain stores.
package store

import (
	"github.com/xraph/blackstart/extensions/scl/device"
	"github.com/xraph/blackstart/pkg/store"
)

// Store is everything the SCL extension persists.
type Store interface {
	store.Lifecycle

	// Devices reads and writes the communication model.
	Devices() device.Store
}
