package scenario

import (
	"context"
	"fmt"

	"github.com/xraph/blackstart/extensions/contingency"
	"github.com/xraph/blackstart/extensions/contingency/sweep"
	scn "github.com/xraph/blackstart/extensions/scenario/scenario"
	solverv1 "github.com/xraph/blackstart/proto/gen/go/blackstart/solver/v1"
)

// screen runs the contingency sweep and picks the outage to cascade.
//
// The two are one step because the second has no meaning without the first:
// the worst candidate is a fact about this sweep, and choosing it anywhere
// else would mean re-ranking a ranking that already exists.
func (e *Engine) screen(
	ctx context.Context,
	m scn.Manifest,
	model *solverv1.NetworkModel,
	overrides []*solverv1.Override,
) (sweep.Run, sweep.Candidate, error) {
	contingencies, err := contingenciesFor(m, model)
	if err != nil {
		return sweep.Run{}, sweep.Candidate{}, err
	}

	screened, err := e.collaborator.Screener.Screen(ctx, contingency.Request{
		Model:         model,
		Contingencies: contingencies,
		Overrides:     overrides,
	})
	if err != nil {
		return sweep.Run{}, sweep.Candidate{}, err
	}

	// Ranked, not Concerning. An unevaluated contingency sorts ahead of every
	// evaluated one and is usually the most consequential thing on the list,
	// and a sweep where nothing is concerning still has a worst outage worth
	// simulating — a scenario that produced no cascade because the screen
	// found nothing alarming would be a scenario with no answer.
	ranked := screened.Ranked()
	if len(ranked) == 0 {
		return sweep.Run{}, sweep.Candidate{}, fmt.Errorf(
			"sweep %s screened no contingencies, so there is no worst outage to "+
				"cascade", screened.ID)
	}

	return screened, ranked[0], nil
}
