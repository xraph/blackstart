package memory

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/xraph/blackstart/extensions/networkmodel/model"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Branches returns the branch store.
func (s *Store) Branches() model.BranchStore { return (*branches)(s) }

type branches Store

func (s *branches) PutAll(_ context.Context, modelID id.ID, in []model.Branch) error {
	for _, branch := range in {
		if err := branch.Validate(); err != nil {
			return err
		}

		if branch.ModelID != modelID {
			return fmt.Errorf(
				"%w: branch %s says it belongs to model %s, not %s",
				bserr.ErrInvalid, branch.ID, branch.ModelID, modelID)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Replaced wholesale. A re-import writes the same ordinals again, and
	// merging would leave two branches answering to one name — which every
	// lookup afterwards resolves arbitrarily.
	s.branches[modelID] = slices.Clone(in)

	return nil
}

func (s *branches) ForModel(_ context.Context, modelID id.ID) ([]model.Branch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := slices.Clone(s.branches[modelID])

	slices.SortFunc(out, func(a, b model.Branch) int {
		return cmp.Compare(ordinalNumber(a.Ordinal), ordinalNumber(b.Ordinal))
	})

	return out, nil
}

func (s *branches) Get(_ context.Context, branchID id.ID) (model.Branch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Scanned rather than indexed. This store exists to back tests and holds
	// one case at a time; a second map keyed by branch would be a second thing
	// PutAll has to keep consistent, for no gain at this size.
	for _, held := range s.branches {
		for _, branch := range held {
			if branch.ID == branchID {
				return branch, nil
			}
		}
	}

	return model.Branch{}, fmt.Errorf(
		"%w: no branch %s", bserr.ErrNotFound, branchID)
}

func (s *branches) ByOrdinal(
	_ context.Context, modelID id.ID, ordinal string,
) (model.Branch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, branch := range s.branches[modelID] {
		if branch.Ordinal == ordinal {
			return branch, nil
		}
	}

	return model.Branch{}, fmt.Errorf(
		"%w: model %s has no %s", bserr.ErrNotFound, modelID, ordinal)
}

// ordinalNumber reads the index out of "branch-10".
//
// Numeric rather than lexical, because sorting these as strings puts
// branch-10 between branch-1 and branch-2 — which is not an ordering anybody
// reading a network expects. An ordinal that does not parse sorts last rather
// than failing: it is a display order, not a correctness property.
func ordinalNumber(ordinal string) int {
	_, digits, found := strings.Cut(ordinal, "-")
	if !found {
		return math.MaxInt
	}

	n, err := strconv.Atoi(digits)
	if err != nil {
		return math.MaxInt
	}

	return n
}
