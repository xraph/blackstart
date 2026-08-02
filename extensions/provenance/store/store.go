// Package store composes the provenance extension's per-domain stores.
//
// One domain package here rather than four, because the four record types are
// one subject: what produced a number. Splitting them would mean a caller
// answering "where did this come from" assembles four stores to ask one
// question.
package store

import (
	"github.com/xraph/blackstart/extensions/provenance/record"
	"github.com/xraph/blackstart/pkg/store"
)

// Store is everything the provenance extension persists.
type Store interface {
	store.Lifecycle

	// Records reads and writes provenance.
	Records() record.Store
}
