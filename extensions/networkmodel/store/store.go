// Package store composes the network-model extension's per-domain stores.
package store

import (
	"context"

	"github.com/xraph/blackstart/extensions/networkmodel/model"
	"github.com/xraph/blackstart/pkg/store"
)

// Store is everything the network-model extension persists.
type Store interface {
	store.Lifecycle

	// Models reads and writes built snapshots.
	Models() model.Store

	// Blobs holds the canonical bytes each model encodes to.
	Blobs() BlobStore
}

// BlobStore holds the canonical encoding of a model.
//
// Separate from the model record because they have different shapes and
// different lifetimes: the record is small and queried constantly, the blob is
// megabytes and read only when a solve needs uploading. Keeping them in one
// row would mean every listing drags the bytes along.
//
// Reference-based rather than keyed by the model's content hash. The durable
// store is Fabriq's content-addressable byte plane, which mints its own
// identifier for what it stores, and inventing a second identity for the same
// bytes is how the two end up disagreeing about which blob a model refers to.
type BlobStore interface {
	// Put stores the bytes and returns the reference to read them back by.
	//
	// The reference goes on the model row. Nothing else knows it, which is the
	// point: a blob with no model pointing at it is unreachable and can be
	// collected.
	Put(ctx context.Context, body []byte) (string, error)

	// Get reads the bytes a reference names.
	Get(ctx context.Context, reference string) ([]byte, error)
}
