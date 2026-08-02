// Package memory is the in-memory implementation of the topology store.
//
// Not a throwaway mock. The engine is tested against this rather than a
// hand-written double per test, so it has to behave like the real store in the
// ways the engine depends on: the same errors, the same filtering, and stable
// ordering.
package memory

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/xraph/blackstart/extensions/topology/network"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

// Store is the in-memory composite.
type Store struct {
	mu sync.RWMutex

	substations map[id.ID]network.Substation
	levels      map[id.ID]network.VoltageLevel
	nodes       map[id.ID]network.ConnectivityNode
	equipment   map[id.ID]network.Equipment
	terminals   map[id.ID]network.Terminal
	aliases     map[string]network.ExternalAlias
}

// New builds an empty store.
func New() *Store {
	return &Store{
		substations: map[id.ID]network.Substation{},
		levels:      map[id.ID]network.VoltageLevel{},
		nodes:       map[id.ID]network.ConnectivityNode{},
		equipment:   map[id.ID]network.Equipment{},
		terminals:   map[id.ID]network.Terminal{},
		aliases:     map[string]network.ExternalAlias{},
	}
}

// Migrate is a no-op: there is no schema to create.
func (s *Store) Migrate(context.Context) error { return nil }

// Ping always succeeds.
func (s *Store) Ping(context.Context) error { return nil }

// Close releases nothing.
func (s *Store) Close() error { return nil }

// Network returns the network store.
func (s *Store) Network() network.Store { return (*net)(s) }

type net Store

func (s *net) CreateSubstation(
	_ context.Context, in network.Substation,
) (network.Substation, error) {
	if err := in.Validate(); err != nil {
		return network.Substation{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.substations[in.ID] = in

	return in, nil
}

func (s *net) GetSubstation(_ context.Context, want id.ID) (network.Substation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	found, ok := s.substations[want]
	if !ok {
		return network.Substation{}, fmt.Errorf("%w: substation %s", bserr.ErrNotFound, want)
	}

	return found, nil
}

func (s *net) ListSubstations(
	_ context.Context, page pkgstore.Page,
) ([]network.Substation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := slices.Collect(maps.Values(s.substations))

	// Sorted by identifier, which is chronologically sortable, so a listing
	// reads the same way every call rather than by map iteration — which Go
	// randomizes on purpose.
	slices.SortFunc(out, func(left, right network.Substation) int {
		return cmp.Compare(left.ID.String(), right.ID.String())
	})

	if page.Limit > 0 && len(out) > page.Limit {
		out = out[:page.Limit]
	}

	return out, nil
}

func (s *net) CreateVoltageLevel(
	_ context.Context, in network.VoltageLevel,
) (network.VoltageLevel, error) {
	if err := in.Validate(); err != nil {
		return network.VoltageLevel{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.levels[in.ID] = in

	return in, nil
}

func (s *net) ListVoltageLevels(
	_ context.Context, f network.Filter,
) ([]network.VoltageLevel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []network.VoltageLevel

	for _, level := range s.levels {
		if !f.SubstationID.IsZero() && level.SubstationID != f.SubstationID {
			continue
		}

		out = append(out, level)
	}

	slices.SortFunc(out, func(left, right network.VoltageLevel) int {
		return cmp.Compare(left.ID.String(), right.ID.String())
	})

	return out, nil
}

func (s *net) CreateNode(
	_ context.Context, in network.ConnectivityNode,
) (network.ConnectivityNode, error) {
	if err := in.Validate(); err != nil {
		return network.ConnectivityNode{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.nodes[in.ID] = in

	return in, nil
}

func (s *net) ListNodes(
	_ context.Context, f network.Filter,
) ([]network.ConnectivityNode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	levels := s.levelsIn(f)

	var out []network.ConnectivityNode

	for _, node := range s.nodes {
		if levels != nil {
			if _, ok := levels[node.VoltageLevelID]; !ok {
				continue
			}
		}

		out = append(out, node)
	}

	slices.SortFunc(out, func(left, right network.ConnectivityNode) int {
		return cmp.Compare(left.ID.String(), right.ID.String())
	})

	return out, nil
}

func (s *net) CreateEquipment(
	_ context.Context, in network.Equipment,
) (network.Equipment, error) {
	if err := in.Validate(); err != nil {
		return network.Equipment{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.equipment[in.ID] = in

	return in, nil
}

func (s *net) GetEquipment(_ context.Context, want id.ID) (network.Equipment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	found, ok := s.equipment[want]
	if !ok {
		return network.Equipment{}, fmt.Errorf("%w: equipment %s", bserr.ErrNotFound, want)
	}

	return found, nil
}

func (s *net) ListEquipment(
	_ context.Context, f network.Filter,
) ([]network.Equipment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	levels := s.levelsIn(f)

	var out []network.Equipment

	for _, item := range s.equipment {
		if levels != nil {
			if _, ok := levels[item.VoltageLevelID]; !ok {
				continue
			}
		}

		if f.Kind != "" && item.Kind != f.Kind {
			continue
		}

		out = append(out, item)
	}

	slices.SortFunc(out, func(left, right network.Equipment) int {
		return cmp.Compare(left.ID.String(), right.ID.String())
	})

	return out, nil
}

func (s *net) CreateTerminal(
	_ context.Context, in network.Terminal,
) (network.Terminal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Both ends must exist. A terminal to a node that was never written is a
	// dangling reference in the structure the whole reduction walks, and the
	// reduction would report it as a malformed snapshot far from its cause.
	if _, ok := s.equipment[in.EquipmentID]; !ok {
		return network.Terminal{}, fmt.Errorf(
			"%w: terminal names equipment %s, which does not exist",
			bserr.ErrInvalid, in.EquipmentID)
	}

	if _, ok := s.nodes[in.ConnectivityNodeID]; !ok {
		return network.Terminal{}, fmt.Errorf(
			"%w: terminal names connectivity node %s, which does not exist",
			bserr.ErrInvalid, in.ConnectivityNodeID)
	}

	s.terminals[in.ID] = in

	return in, nil
}

func (s *net) ListTerminals(
	_ context.Context, f network.Filter,
) ([]network.Terminal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	levels := s.levelsIn(f)

	var out []network.Terminal

	for _, terminal := range s.terminals {
		if levels != nil {
			node, ok := s.nodes[terminal.ConnectivityNodeID]
			if !ok {
				continue
			}

			if _, ok := levels[node.VoltageLevelID]; !ok {
				continue
			}
		}

		out = append(out, terminal)
	}

	// Ordered by equipment then sequence, because that is the order a snapshot
	// needs: a branch's two terminals must come back with the from-end first
	// or the reduction sees a different network.
	slices.SortFunc(out, func(left, right network.Terminal) int {
		if by := cmp.Compare(left.EquipmentID.String(), right.EquipmentID.String()); by != 0 {
			return by
		}

		return cmp.Compare(left.Sequence, right.Sequence)
	})

	return out, nil
}

func (s *net) CreateAlias(
	_ context.Context, in network.ExternalAlias,
) (network.ExternalAlias, error) {
	if err := in.Validate(); err != nil {
		return network.ExternalAlias{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.aliases[in.Namespace+"\x00"+in.Value] = in

	return in, nil
}

func (s *net) ResolveAlias(
	_ context.Context, namespace, value string,
) (network.ExternalAlias, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	found, ok := s.aliases[namespace+"\x00"+value]
	if !ok {
		return network.ExternalAlias{}, fmt.Errorf(
			"%w: no %s alias for %q", bserr.ErrNotFound, namespace, value)
	}

	return found, nil
}

// levelsIn resolves a filter to the set of voltage levels it selects, or nil
// when it selects everything.
//
// Resolved once per listing rather than per row, because a substation filter
// otherwise means a lookup for every node in the store.
func (s *net) levelsIn(f network.Filter) map[id.ID]struct{} {
	if !f.VoltageLevelID.IsZero() {
		return map[id.ID]struct{}{f.VoltageLevelID: {}}
	}

	if f.SubstationID.IsZero() {
		return nil
	}

	out := map[id.ID]struct{}{}

	for _, level := range s.levels {
		if level.SubstationID == f.SubstationID {
			out[level.ID] = struct{}{}
		}
	}

	return out
}
