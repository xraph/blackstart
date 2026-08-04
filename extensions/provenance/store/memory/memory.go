// Package memory is the in-memory implementation of the provenance store.
//
// Not a throwaway mock. The engine is tested against this rather than against a
// hand-written double per test, so it has to behave like the real store in the
// ways the engine depends on: the same errors, the same append-only rule, and
// the same content-hash lookup.
package memory

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/xraph/blackstart/extensions/provenance/record"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Store is the in-memory composite.
type Store struct {
	mu sync.RWMutex

	datasets map[id.ID]record.DatasetVersion
	models   map[id.ID]record.ModelVersion
	runs     map[id.ID]record.Run
	lineage  []record.Lineage

	// byHash indexes datasets by content hash, because "have we already
	// retrieved these exact bytes" is the question every ingest asks first and
	// a scan would make re-download detection cost more than re-downloading.
	byHash map[string]id.ID
}

// New builds an empty store.
func New() *Store {
	return &Store{
		datasets: map[id.ID]record.DatasetVersion{},
		models:   map[id.ID]record.ModelVersion{},
		runs:     map[id.ID]record.Run{},
		byHash:   map[string]id.ID{},
	}
}

// Migrate is a no-op: there is no schema to create.
func (s *Store) Migrate(context.Context) error { return nil }

// Ping always succeeds. An in-memory store is reachable whenever the process
// is, so reporting anything else would be inventing a failure mode.
func (s *Store) Ping(context.Context) error { return nil }

// Close releases nothing.
func (s *Store) Close() error { return nil }

// Records returns the record store.
func (s *Store) Records() record.Store { return (*records)(s) }

type records Store

// CreateDataset records a retrieval.
func (s *records) CreateDataset(
	_ context.Context, in record.DatasetVersion,
) (record.DatasetVersion, error) {
	if err := in.Validate(); err != nil {
		return record.DatasetVersion{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.datasets[in.ID]; exists {
		return record.DatasetVersion{}, fmt.Errorf(
			"%w: dataset version %s already exists; provenance is append-only",
			bserr.ErrConflict, in.ID)
	}

	// The same bytes are the same version, whatever the identifier and
	// timestamp on this retrieval say. Returning the existing record rather
	// than writing a second one is what makes a re-download that changed
	// nothing cheap: there is no new version, so nothing derived from the
	// previous one is invalidated.
	//
	// The Fabriq store gets this from a unique index on (tenant_id,
	// content_hash), and this store has to agree: the engine is tested here
	// and deployed there, so a divergence would be a suite describing
	// behaviour the deployment does not have.
	if existing, seen := s.byHash[in.ContentHash]; seen {
		return s.datasets[existing], nil
	}

	s.datasets[in.ID] = in
	s.byHash[in.ContentHash] = in.ID

	return in, nil
}

// GetDataset retrieves one retrieval.
func (s *records) GetDataset(_ context.Context, want id.ID) (record.DatasetVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	found, ok := s.datasets[want]
	if !ok {
		return record.DatasetVersion{}, fmt.Errorf(
			"%w: dataset version %s", bserr.ErrNotFound, want)
	}

	return found, nil
}

// FindDatasetByHash answers "have we already retrieved these exact bytes".
func (s *records) FindDatasetByHash(
	_ context.Context, contentHash string,
) (record.DatasetVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key, ok := s.byHash[contentHash]
	if !ok {
		return record.DatasetVersion{}, fmt.Errorf(
			"%w: no dataset version with content hash %s", bserr.ErrNotFound, contentHash)
	}

	return s.datasets[key], nil
}

// CreateModel records a producer version.
func (s *records) CreateModel(
	_ context.Context, in record.ModelVersion,
) (record.ModelVersion, error) {
	if err := in.Validate(); err != nil {
		return record.ModelVersion{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.models[in.ID]; exists {
		return record.ModelVersion{}, fmt.Errorf(
			"%w: model version %s already exists", bserr.ErrConflict, in.ID)
	}

	s.models[in.ID] = in

	return in, nil
}

// GetModel retrieves one producer version.
func (s *records) GetModel(_ context.Context, want id.ID) (record.ModelVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	found, ok := s.models[want]
	if !ok {
		return record.ModelVersion{}, fmt.Errorf(
			"%w: model version %s", bserr.ErrNotFound, want)
	}

	return found, nil
}

// CreateRun records an execution.
func (s *records) CreateRun(_ context.Context, in record.Run) (record.Run, error) {
	if err := in.Validate(); err != nil {
		return record.Run{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.runs[in.ID]; exists {
		return record.Run{}, fmt.Errorf(
			"%w: run %s already exists", bserr.ErrConflict, in.ID)
	}

	// A run naming a producer that was never recorded would be a broken chain,
	// and a broken chain is worse than a missing one: it looks traceable.
	if _, ok := s.models[in.ModelVersionID]; !ok {
		return record.Run{}, fmt.Errorf(
			"%w: run %s names model version %s, which was never recorded",
			bserr.ErrInvalid, in.ID, in.ModelVersionID)
	}

	s.runs[in.ID] = in

	return in, nil
}

// GetRun retrieves one execution.
func (s *records) GetRun(_ context.Context, want id.ID) (record.Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	found, ok := s.runs[want]
	if !ok {
		return record.Run{}, fmt.Errorf("%w: run %s", bserr.ErrNotFound, want)
	}

	return found, nil
}

// CreateLineage records that a run used a dataset.
func (s *records) CreateLineage(
	_ context.Context, in record.Lineage,
) (record.Lineage, error) {
	if err := in.Validate(); err != nil {
		return record.Lineage{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Both ends have to exist. An edge to a record that was never written is a
	// dangling reference in the one structure whose whole value is that it can
	// be followed.
	if _, ok := s.runs[in.RunID]; !ok {
		return record.Lineage{}, fmt.Errorf(
			"%w: lineage names run %s, which was never recorded",
			bserr.ErrInvalid, in.RunID)
	}

	if _, ok := s.datasets[in.DatasetVersionID]; !ok {
		return record.Lineage{}, fmt.Errorf(
			"%w: lineage names dataset version %s, which was never recorded",
			bserr.ErrInvalid, in.DatasetVersionID)
	}

	s.lineage = append(s.lineage, in)

	return in, nil
}

// LineageOf returns every input a run used, in the order recorded.
func (s *records) LineageOf(_ context.Context, runID id.ID) ([]record.Lineage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []record.Lineage

	for _, edge := range s.lineage {
		if edge.RunID == runID {
			out = append(out, edge)
		}
	}

	// Insertion order, cloned so a caller cannot mutate the store's slice
	// through the one it was handed.
	return slices.Clone(out), nil
}
