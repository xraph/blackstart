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
		Model: model,
		// The manifest's pinned hash, which resolve verified against the bytes
		// it decoded — so this is not a claim, it is the hash the model on the
		// line above actually has. contingency cannot compute it: it holds a
		// wire model, and the canonical encoding is already gone by then.
		//
		// Without it the sweep records only the solver's protobuf hash, and
		// "every sweep against the network this scenario pins" has no answer:
		// the two are hashes over different encodings, so the string an
		// operator reads off the manifest matches nothing.
		CanonicalHash: m.ModelHash,
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

	worst, found := worstApplicable(ranked)
	if !found {
		return sweep.Run{}, sweep.Candidate{}, fmt.Errorf(
			"sweep %s screened %d contingencies and every one of them names a branch "+
				"the pinned model already has out of service, so there is no outage to "+
				"cascade: opening an open branch would produce a cascade in which "+
				"nothing happens and present it as this scenario's answer",
			screened.ID, len(ranked))
	}

	return screened, worst, nil
}

// worstApplicable is ranked[0] with the already-open branches passed over.
//
// Ranking's leading bucket is everything the screen could not evaluate, and
// that bucket holds two unlike things. sweep.Radial belongs at the front: a
// real outage a linear factor cannot answer, so it has to be simulated.
// sweep.NotApplicable does not — sweep.go says of it that "the branch was
// already out of service, so its outage is not a contingency at all", and
// cascading one opens a branch that is already open. The run would complete,
// link a real casc_…, and show a cascade in which nothing happens, while the
// contingencies the manifest actually named went unsimulated. A false
// all-clear is the one answer worse here than no answer.
//
// Only PolicyExplicit can reach this. Rust's all_branches filters to
// in-service before it screens anything, so PolicyAll never produces the
// bucket; the explicit path deliberately does no in-service filtering,
// because §5 makes the ordering the caller's, and rate.go keeps and rates
// out-of-service branches on purpose. Each of those is right on its own,
// which is why the gap between them has to be closed here rather than in any
// of them.
func worstApplicable(ranked []sweep.Candidate) (sweep.Candidate, bool) {
	for _, candidate := range ranked {
		if candidate.Screenability != sweep.NotApplicable {
			return candidate, true
		}
	}

	return sweep.Candidate{}, false
}
