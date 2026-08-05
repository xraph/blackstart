package hazard

import (
	"context"
	"fmt"

	"github.com/xraph/blackstart/extensions/hazard/conductor"
	hid "github.com/xraph/blackstart/extensions/hazard/id"
)

// Assume gives a conductor to every carrier that states a voltage and has none.
//
// The decision is [conductor.PlanAssumptions] and is pure; this adds the two
// things a decision cannot do for itself — mint identifiers and write rows.
// Splitting it that way is what lets a dry run be the same computation as the
// run it precedes, rather than a second code path that agrees until one of
// them is edited.
//
// Nothing is overwritten. A carrier already assigned is reported and left,
// because a surveyed row outranks an assumption and a re-run that quietly
// demoted one would destroy the only record of somebody having looked.
//
// A carrier whose source stated no voltage is reported and left too. Giving it
// a default would record assumed_by_voltage where the voltage was not the
// element's, which spends the basis field — the entire mechanism by which a
// consumer knows how far to trust a rating — to raise a coverage number.
//
// # Errors
//
// Returns an error naming the carrier whose assignment could not be stored.
// Earlier writes stand: the pass is a sequence of independent facts, not a
// transaction, and rolling back a hundred good assignments because the
// hundred-and-first failed would lose more than it protected.
func (e *Engine) Assume(
	ctx context.Context, candidates []conductor.Candidate, dryRun bool,
) (conductor.Plan, error) {
	plan := conductor.PlanAssumptions(candidates)

	if dryRun {
		return plan, nil
	}

	for _, assumption := range plan.Assume {
		if err := e.Assign(ctx, conductor.Assignment{
			ID:            hid.NewAssignment(),
			CarrierID:     assumption.CarrierID,
			ConductorCode: assumption.ConductorCode,
			Basis:         conductor.AssumedByVoltage,
		}); err != nil {
			return plan, fmt.Errorf(
				"hazard: assuming %s for carrier %s: %w",
				assumption.ConductorCode, assumption.CarrierID, err)
		}
	}

	return plan, nil
}
