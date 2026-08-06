package memory

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/xraph/blackstart/extensions/networkmodel/model"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Buses returns the bus store.
func (s *Store) Buses() model.BusStore { return (*buses)(s) }

type buses Store

func (s *buses) PutAll(_ context.Context, modelID id.ID, in []model.Bus) error {
	for _, bus := range in {
		if err := bus.Validate(); err != nil {
			return err
		}

		if bus.ModelID != modelID {
			return fmt.Errorf(
				"%w: bus %s says it belongs to model %s, not %s",
				bserr.ErrInvalid, bus.ID, bus.ModelID, modelID)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Replaced wholesale, exactly as branches are. A re-import writes the same
	// ordinals again, and merging would leave two buses answering to one name.
	s.buses[modelID] = slices.Clone(in)

	return nil
}

func (s *buses) ForModel(_ context.Context, modelID id.ID) ([]model.Bus, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := slices.Clone(s.buses[modelID])

	// ordinalNumber is shared with the branch store in this package: "bus-10"
	// and "branch-10" have the same shape and the same sorting hazard.
	slices.SortFunc(out, func(a, b model.Bus) int {
		return cmp.Compare(ordinalNumber(a.Ordinal), ordinalNumber(b.Ordinal))
	})

	return out, nil
}

func (s *buses) ByOrdinal(
	_ context.Context, modelID id.ID, ordinal string,
) (model.Bus, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, bus := range s.buses[modelID] {
		if bus.Ordinal == ordinal {
			return bus, nil
		}
	}

	return model.Bus{}, fmt.Errorf(
		"%w: model %s has no %s", bserr.ErrNotFound, modelID, ordinal)
}
