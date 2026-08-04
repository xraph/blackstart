package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"

	"github.com/xraph/blackstart/extensions/weather/store"
	"github.com/xraph/blackstart/pkg/bserr"
)

// Blobs returns the blob store.
func (s *Store) Blobs() store.BlobStore { return (*blobs)(s) }

type blobs Store

// Put stores bytes under their own content hash.
//
// Content-addressed like the Fabriq store it stands in for, so identical bytes
// stored twice are one entry and the reference a caller gets back is stable —
// an engine test that asserted a reference would otherwise pass against this
// and fail against production.
func (s *blobs) Put(_ context.Context, body []byte) (string, error) {
	sum := sha256.Sum256(body)
	reference := "sha256:" + hex.EncodeToString(sum[:])

	s.mu.Lock()
	defer s.mu.Unlock()

	s.blobs[reference] = slices.Clone(body)

	return reference, nil
}

func (s *blobs) Get(_ context.Context, reference string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	body, ok := s.blobs[reference]
	if !ok {
		return nil, fmt.Errorf("%w: blob %s", bserr.ErrNotFound, reference)
	}

	return slices.Clone(body), nil
}
