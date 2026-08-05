// Package memory is the in-memory implementation of the cascade store.
//
// Not a throwaway mock. The engine is tested against this rather than against
// a hand-written double per test, so it has to behave like the real store in
// the ways the engine depends on: the same errors, the same append-only rule,
// and the same ordering. Mirrors extensions/contingency/store/memory exactly,
// substituting run.CascadeRun for sweep.Run.
package memory

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/xraph/blackstart/extensions/cascade/run"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Store is the in-memory composite.
type Store struct {
	mu sync.RWMutex

	runs map[id.ID]run.CascadeRun

	// order preserves insertion sequence, so a list can be built without
	// depending on map iteration — which Go deliberately randomizes, and
	// which would make every list a different list.
	order []id.ID
}

// New builds an empty store.
func New() *Store {
	return &Store{runs: map[id.ID]run.CascadeRun{}}
}

// Migrate is a no-op: there is no schema to create.
func (s *Store) Migrate(context.Context) error { return nil }

// Ping always succeeds. An in-memory store is reachable whenever the process
// is, so reporting anything else would be inventing a failure mode.
func (s *Store) Ping(context.Context) error { return nil }

// Close releases nothing.
func (s *Store) Close() error { return nil }

// Runs returns the run store.
func (s *Store) Runs() run.Store { return (*runs)(s) }

type runs Store

// Create records a run, rejecting a duplicate identifier rather than
// overwriting: runs are append-only, and an overwrite would replace results
// someone may already have cited.
func (s *runs) Create(_ context.Context, in run.CascadeRun) (run.CascadeRun, error) {
	if err := in.Validate(); err != nil {
		return run.CascadeRun{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.runs[in.ID]; exists {
		return run.CascadeRun{}, fmt.Errorf(
			"%w: run %s already exists; runs are append-only", bserr.ErrConflict, in.ID)
	}

	s.runs[in.ID] = in
	s.order = append(s.order, in.ID)

	return in, nil
}

// Get retrieves one run.
func (s *runs) Get(_ context.Context, runID id.ID) (run.CascadeRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	found, ok := s.runs[runID]
	if !ok {
		return run.CascadeRun{}, fmt.Errorf("%w: run %s", bserr.ErrNotFound, runID)
	}

	return found, nil
}

// ListByModel returns runs for one network, most recent first.
//
// A limit of zero or less returns everything, which is what a CLI wants.
func (s *runs) ListByModel(
	_ context.Context, modelHash string, limit int,
) ([]run.CascadeRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []run.CascadeRun

	for _, key := range s.order {
		if found := s.runs[key]; found.ModelHash == modelHash {
			out = append(out, found)
		}
	}

	slices.Reverse(out)

	// Ties on StartedAt break on identifier, which is chronologically
	// sortable, so two runs recorded in the same instant still order
	// reproducibly.
	slices.SortStableFunc(out, func(left, right run.CascadeRun) int {
		if by := right.StartedAt.Compare(left.StartedAt); by != 0 {
			return by
		}

		return cmp.Compare(right.ID.String(), left.ID.String())
	})

	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}

	return out, nil
}
