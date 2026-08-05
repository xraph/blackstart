// Package memory is the in-memory implementation of the scenario store.
//
// Not a throwaway mock. Whatever exercises the scenario engine is tested
// against this rather than against a hand-written double per test, so it has
// to behave like the real store in the ways callers depend on: the same
// errors, the same ordering, and — for Claim, in runs.go — the same
// atomicity. Mirrors extensions/cascade/store/memory, substituting two
// domain accessors for its one.
//
// Split across two files by domain rather than kept in one: this one holds
// the composite and the scenario accessor, runs.go holds the run accessor.
// Both still add up to one implementation of one package.
//
// # This store aliases slices and the Fabriq one does not
//
// A read here returns the stored struct by value, so a caller holding the
// result shares the backing array of every slice in it — Manifest.Branches
// above all. Editing that slice edits the stored record. The Fabriq store
// cannot behave this way: its manifest round-trips through a JSON column, so
// every read is a fresh allocation.
//
// The divergence is left in place deliberately. It is what makes
// TestEnqueueFreezesTheBranchListToo bite: that test edits a scenario's
// branch list in place and asserts a run frozen beforehand did not follow,
// and it is the only test in the extension that can catch Enqueue taking a
// shallow copy of a manifest. Cloning on read here would make it pass
// vacuously and remove the guard on the invariant the extension exists for.
//
// So: a test relying on this aliasing is relying on a memory-store property,
// not on a store contract. Anything that must hold for both stores has to be
// written so it holds against a store that copies.
package memory

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/xraph/blackstart/extensions/scenario/scenario"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Store is the in-memory composite.
type Store struct {
	mu sync.RWMutex

	scenarios map[id.ID]scenario.Scenario

	// scenarioOrder preserves insertion sequence, so a list can be built
	// without depending on map iteration — which Go deliberately randomizes.
	scenarioOrder []id.ID

	runs map[id.ID]scenario.Run

	// runOrder is the same device as scenarioOrder: it lets ListByScenario
	// build a result without depending on map iteration order.
	//
	// It is not Claim's queue order. A run's insertion position is when it
	// was recorded, and Claim's queue is ordered by QueuedAt — the two agree
	// only when nothing is ever backfilled. A run queued for 09:00 but
	// created after one queued for 10:00 sorts after it here, and Claim
	// (runs.go) scans every candidate's QueuedAt rather than trusting this
	// slice's position, for exactly that reason.
	runOrder []id.ID
}

// New builds an empty store.
func New() *Store {
	return &Store{
		scenarios: map[id.ID]scenario.Scenario{},
		runs:      map[id.ID]scenario.Run{},
	}
}

// Migrate is a no-op: there is no schema to create.
func (s *Store) Migrate(context.Context) error { return nil }

// Ping always succeeds. An in-memory store is reachable whenever the process
// is, so reporting anything else would be inventing a failure mode.
func (s *Store) Ping(context.Context) error { return nil }

// Close releases nothing.
func (s *Store) Close() error { return nil }

// Scenarios returns the scenario store.
func (s *Store) Scenarios() scenario.Store { return (*scenarios)(s) }

// Runs returns the run store. Defined in runs.go, alongside the rest of the
// run accessor — this file only holds the fields runs.go's methods need.

type scenarios Store

// Create records a scenario, rejecting a duplicate identifier rather than
// overwriting it.
func (s *scenarios) Create(
	_ context.Context, in scenario.Scenario,
) (scenario.Scenario, error) {
	if err := in.Manifest.Validate(); err != nil {
		return scenario.Scenario{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.scenarios[in.ID]; exists {
		return scenario.Scenario{}, fmt.Errorf(
			"%w: scenario %s already exists", bserr.ErrConflict, in.ID)
	}

	s.scenarios[in.ID] = in
	s.scenarioOrder = append(s.scenarioOrder, in.ID)

	return in, nil
}

// Get retrieves one scenario.
func (s *scenarios) Get(_ context.Context, scenarioID id.ID) (scenario.Scenario, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	found, ok := s.scenarios[scenarioID]
	if !ok {
		return scenario.Scenario{}, fmt.Errorf("%w: scenario %s", bserr.ErrNotFound, scenarioID)
	}

	return found, nil
}

// Update replaces a stored scenario, refusing to create one that is not
// already there — the same rule the Fabriq store enforces, backed here by a
// map lookup rather than by the row count of an UPDATE.
func (s *scenarios) Update(
	_ context.Context, in scenario.Scenario,
) (scenario.Scenario, error) {
	if err := in.Manifest.Validate(); err != nil {
		return scenario.Scenario{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.scenarios[in.ID]; !exists {
		return scenario.Scenario{}, fmt.Errorf(
			"%w: scenario %s", bserr.ErrNotFound, in.ID)
	}

	s.scenarios[in.ID] = in

	return in, nil
}

// List returns scenarios, most recently created first.
//
// A limit of zero or less returns everything, matching scenario.Store's own
// documentation and the Fabriq store's behavior.
func (s *scenarios) List(_ context.Context, limit int) ([]scenario.Scenario, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]scenario.Scenario, 0, len(s.scenarioOrder))
	for _, key := range s.scenarioOrder {
		out = append(out, s.scenarios[key])
	}

	// Ties on CreatedAt break on identifier, which is chronologically
	// sortable, so two scenarios defined in the same instant still order
	// reproducibly — and so this store and the Fabriq one, which asks
	// Postgres for `ORDER BY created_at DESC, id DESC`, agree.
	slices.SortStableFunc(out, func(left, right scenario.Scenario) int {
		if by := right.CreatedAt.Compare(left.CreatedAt); by != 0 {
			return by
		}

		return cmp.Compare(right.ID.String(), left.ID.String())
	})

	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}

	return out, nil
}
