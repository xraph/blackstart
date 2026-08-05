package fabriq

import (
	"context"
	"fmt"

	"github.com/xraph/blackstart/extensions/geo/asset"
	"github.com/xraph/blackstart/extensions/geo/store"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Structures returns the structure store.
//
// Stubbed pending its own task: this satisfies store.Store so the composite
// still compiles, but every call refuses rather than silently discarding the
// structures a caller tried to write.
func (s *Store) Structures() store.StructureStore { return (*structures)(s) }

type structures Store

func (s *structures) Put(context.Context, id.ID, []asset.Structure) error {
	return fmt.Errorf("%w: geo/fabriq structure store is not yet implemented", bserr.ErrUnsupported)
}

func (s *structures) ForRoute(context.Context, id.ID) ([]asset.Structure, error) {
	return nil, fmt.Errorf("%w: geo/fabriq structure store is not yet implemented", bserr.ErrUnsupported)
}

func (s *structures) Get(context.Context, id.ID) (asset.Structure, error) {
	return asset.Structure{}, fmt.Errorf("%w: geo/fabriq structure store is not yet implemented", bserr.ErrUnsupported)
}
