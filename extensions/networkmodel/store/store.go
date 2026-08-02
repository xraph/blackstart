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

// BlobStore holds the canonical encoding of a model, keyed by content hash.
//
// Separate from the model record because they have different shapes and
// different lifetimes: the record is small and queried constantly, the blob is
// megabytes and read only when a solve needs uploading. Keeping them in one
// row would mean every listing drags the bytes along.
type BlobStore interface {
	Put(ctx context.Context, contentHash string, body []byte) error
	Get(ctx context.Context, contentHash string) ([]byte, error)
}
