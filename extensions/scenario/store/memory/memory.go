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
package memory

import (
	"context"
	"fmt"
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

	// runOrder is the same device as scenarioOrder, and doubles as Claim's
	// queue order: runs are created in chronological order, so the first
	// entry still queued is the oldest one.
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
