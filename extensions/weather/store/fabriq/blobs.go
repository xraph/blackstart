package fabriq

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/xraph/fabriq"

	"github.com/xraph/blackstart/extensions/weather/store"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/store/fabriqstore"
)

// Blobs returns the blob store.
func (s *Store) Blobs() store.BlobStore { return &blobStore{base: s.base} }

type blobStore struct {
	base *fabriqstore.Base
}

// gribContentType names what these bytes are, so an operator listing the
// object store sees a format rather than an opaque blob.
const gribContentType = "application/x-grib2"

// Put stores a retrieved message in Fabriq's content-addressable byte plane.
//
// Not a table of our own — the plane is trove-backed CAS, so identical bytes
// are stored once and ref-counted, and the catalog row it writes goes through
// the command path with the same tenancy and audit treatment as any other
// write. The pattern, and the hash cross-check below, follow
// networkmodel/store/fabriq.
//
// The returned reference is checked against the hash computed here. Trove
// reports "sha256:<hex>" and this is the same digest of the same bytes, so a
// mismatch means something re-encoded them between the hash and the write —
// which would leave a grid naming bytes it was not hashed from, and a
// DatasetVersion pointing at a retrieval that is not this one.
func (s *blobStore) Put(ctx context.Context, body []byte) (string, error) {
	ref, err := s.base.Fabric().PutBlob(s.base.Scoped(ctx), bytes.NewReader(body),
		fabriq.PutBlobOpts{ContentType: gribContentType})
	if err != nil {
		return "", fmt.Errorf("weather: storing a retrieved message: %w",
			fabriqstore.Translate(err))
	}

	sum := sha256.Sum256(body)

	if want := "sha256:" + hex.EncodeToString(sum[:]); ref.Hash != want {
		return "", fmt.Errorf(
			"%w: the byte plane stored %s but these bytes hash to %s; something "+
				"altered them between hashing and writing, and the grid would name "+
				"bytes it was not hashed from",
			bserr.ErrConflict, ref.Hash, want)
	}

	return ref.ID, nil
}

// Get reads the bytes a reference names.
func (s *blobStore) Get(ctx context.Context, reference string) ([]byte, error) {
	reader, _, err := s.base.Fabric().GetBlob(s.base.Scoped(ctx), reference)
	if err != nil {
		return nil, fmt.Errorf("weather: reading a stored message: %w",
			fabriqstore.Translate(err))
	}

	defer func() { _ = reader.Close() }()

	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("weather: reading a stored message: %w", err)
	}

	return body, nil
}
