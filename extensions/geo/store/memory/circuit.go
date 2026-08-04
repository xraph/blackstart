package memory

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/xraph/blackstart/extensions/geo/asset"
	"github.com/xraph/blackstart/extensions/geo/store"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Circuits returns the circuit store.
func (s *Store) Circuits() store.CircuitStore { return (*circuits)(s) }

type circuits Store

// externalKey is the reconciliation key: what the source called it, and which
// source that was. Neither half identifies a circuit alone — OSM way 4625 and
// HIFLD feature 4625 are different lines that happen to share a number.
type externalKey struct {
	source   string
	external string
}

func (c *circuits) Reconcile(_ context.Context, circuit asset.Circuit) (asset.Circuit, error) {
	if err := circuit.Validate(); err != nil {
		return asset.Circuit{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	key := externalKey{source: circuit.Source, external: circuit.ExternalID}

	// The existing identifier wins over the one the caller brought. A route
	// already points at it, and taking the new one would leave that geometry
	// attached to a circuit nothing else refers to.
	if existing, ok := c.byExternal[key]; ok {
		circuit.ID = existing
	}

	c.circuits[circuit.ID] = circuit
	c.byExternal[key] = circuit.ID

	return circuit, nil
}

func (c *circuits) Get(_ context.Context, want id.ID) (asset.Circuit, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	found, ok := c.circuits[want]
	if !ok {
		return asset.Circuit{}, fmt.Errorf("%w: circuit %s", bserr.ErrNotFound, want)
	}

	return found, nil
}

func (c *circuits) ByExternal(_ context.Context, source, external string) (asset.Circuit, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	found, ok := c.byExternal[externalKey{source: source, external: external}]
	if !ok {
		return asset.Circuit{}, fmt.Errorf(
			"%w: no circuit from %s with external identifier %s",
			bserr.ErrNotFound, source, external)
	}

	return c.circuits[found], nil
}

func (c *circuits) List(_ context.Context, limit int) ([]asset.Circuit, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := slices.Collect(maps.Values(c.circuits))

	slices.SortFunc(out, func(left, right asset.Circuit) int {
		return cmp.Compare(left.ID.String(), right.ID.String())
	})

	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}

	return out, nil
}
