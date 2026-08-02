// Package memory is the in-memory implementation of the network-model store.
//
// Not a throwaway mock. The engine is tested against this rather than a
// hand-written double per test, so it has to behave like the real store in the
// ways the engine depends on: the same errors, the same append-only rule, and
// the same content-hash lookup.
package memory

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/xraph/blackstart/extensions/networkmodel/model"
	"github.com/xraph/blackstart/extensions/networkmodel/store"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Store is the in-memory composite.
type Store struct {
	mu sync.RWMutex

	models map[id.ID]model.NetworkModel
	byHash map[string]id.ID
	blobs  map[string][]byte
}

// New builds an empty store.
func New() *Store {
	return &Store{
		models: map[id.ID]model.NetworkModel{},
		byHash: map[string]id.ID{},
		blobs:  map[string][]byte{},
	}
}

// Migrate is a no-op: there is no schema to create.
func (s *Store) Migrate(context.Context) error { return nil }

// Ping always succeeds.
func (s *Store) Ping(context.Context) error { return nil }

// Close releases nothing.
func (s *Store) Close() error { return nil }

// Models returns the model store.
func (s *Store) Models() model.Store { return (*models)(s) }

// Blobs returns the blob store.
func (s *Store) Blobs() store.BlobStore { return (*blobs)(s) }

type models Store

func (s *models) Create(_ context.Context, in model.NetworkModel) (model.NetworkModel, error) {
	if err := in.Validate(); err != nil {
		return model.NetworkModel{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.models[in.ID]; exists {
		return model.NetworkModel{}, fmt.Errorf(
			"%w: network model %s already exists; models are append-only",
			bserr.ErrConflict, in.ID)
	}

	s.models[in.ID] = in

	// First writer wins on the hash index. Two models with one hash describe
	// the same network, so pointing at the later one would change what an
	// existing result's hash resolves to.
	if _, seen := s.byHash[in.ContentHash]; !seen {
		s.byHash[in.ContentHash] = in.ID
	}

	return in, nil
}

func (s *models) Get(_ context.Context, want id.ID) (model.NetworkModel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	found, ok := s.models[want]
	if !ok {
		return model.NetworkModel{}, fmt.Errorf("%w: network model %s", bserr.ErrNotFound, want)
	}

	return found, nil
}

func (s *models) FindByHash(_ context.Context, contentHash string) (model.NetworkModel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key, ok := s.byHash[contentHash]
	if !ok {
		return model.NetworkModel{}, fmt.Errorf(
			"%w: no network model with content hash %s", bserr.ErrNotFound, contentHash)
	}

	return s.models[key], nil
}

func (s *models) SetBlobRef(
	_ context.Context, want id.ID, reference string,
) (model.NetworkModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	found, ok := s.models[want]
	if !ok {
		return model.NetworkModel{}, fmt.Errorf("%w: model %s", bserr.ErrNotFound, want)
	}

	found.BlobRef = reference
	s.models[want] = found

	return found, nil
}

func (s *models) List(_ context.Context, limit int) ([]model.NetworkModel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := slices.Collect(maps.Values(s.models))

	// Most recent first, ties broken on identifier — which is chronologically
	// sortable, so two models built in one instant still order reproducibly
	// rather than by map iteration, which Go randomizes on purpose.
	slices.SortFunc(out, func(left, right model.NetworkModel) int {
		if by := right.BuiltAt.Compare(left.BuiltAt); by != 0 {
			return by
		}

		return cmp.Compare(right.ID.String(), left.ID.String())
	})

	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}

	return out, nil
}

type blobs Store

func (s *blobs) Put(_ context.Context, body []byte) (string, error) {
	// Content-addressed, matching the durable store: identical bytes get one
	// reference and one copy. A counter would work for a fake, and would let a
	// test pass that the real store fails — the real one deduplicates, so a
	// second import of one file must not produce a second reference.
	sum := sha256.Sum256(body)
	reference := "sha256:" + hex.EncodeToString(sum[:])

	s.mu.Lock()
	defer s.mu.Unlock()

	s.blobs[reference] = bytes.Clone(body)

	return reference, nil
}

func (s *blobs) Get(_ context.Context, reference string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	body, ok := s.blobs[reference]
	if !ok {
		return nil, fmt.Errorf("%w: no blob for %s", bserr.ErrNotFound, reference)
	}

	return bytes.Clone(body), nil
}
