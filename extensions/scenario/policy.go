package scenario

import (
	"fmt"

	scn "github.com/xraph/blackstart/extensions/scenario/scenario"
	solverv1 "github.com/xraph/blackstart/proto/gen/go/blackstart/solver/v1"
)

// contingenciesFor turns the manifest's policy into a contingency list.
//
// Nil for PolicyAll, which the contingency contract reads as every in-service
// branch — the default, and not the same as an empty list meaning nothing.
// For PolicyExplicit the manifest's own order is preserved: the ordering is
// the caller's deliberately, because this is where a risk model plugs in, and
// re-sorting it here would quietly discard the one thing the policy said.
func contingenciesFor(
	m scn.Manifest, model *solverv1.NetworkModel,
) ([]*solverv1.Contingency, error) {
	if m.Policy != scn.PolicyExplicit {
		return nil, nil
	}

	branches := model.GetBranches()

	at := make(map[string]uint32, len(branches))
	for index, branch := range branches {
		at[branch.GetId()] = uint32(index)
	}

	out := make([]*solverv1.Contingency, 0, len(m.Branches))

	for _, named := range m.Branches {
		index, present := at[named]
		if !present {
			return nil, fmt.Errorf(
				"the policy names branch %q, which the pinned model's %d branches do "+
					"not include; screening the rest would answer a question nobody "+
					"asked", named, len(branches))
		}

		out = append(out, &solverv1.Contingency{Id: named, Branch: index})
	}

	return out, nil
}
