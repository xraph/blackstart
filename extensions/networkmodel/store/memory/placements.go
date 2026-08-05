package memory

import (
	"context"
	"fmt"
	"slices"

	"github.com/xraph/blackstart/extensions/networkmodel/model"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Placements returns the placement store.
func (s *Store) Placements() model.PlacementStore { return (*placements)(s) }

type placements Store

func (s *placements) PutAll(_ context.Context, modelID id.ID, in []model.Placement) error {
	for _, placement := range in {
		if err := placement.Validate(); err != nil {
			return err
		}

		if placement.ModelID != modelID {
			return fmt.Errorf(
				"%w: placement %s says it belongs to model %s, not %s",
				bserr.ErrInvalid, placement.ID, placement.ModelID, modelID)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.placements[modelID] = slices.Clone(in)

	return nil
}

func (s *placements) ForModel(
	_ context.Context, modelID id.ID,
) ([]model.Placement, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return slices.Clone(s.placements[modelID]), nil
}
