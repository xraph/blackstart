package scenario

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/blackstart/extensions/networkmodel/canonical"
	"github.com/xraph/blackstart/extensions/networkmodel/wire"
	scn "github.com/xraph/blackstart/extensions/scenario/scenario"
	wxid "github.com/xraph/blackstart/extensions/weather/id"
	solverv1 "github.com/xraph/blackstart/proto/gen/go/blackstart/solver/v1"
)

// resolvedManifest is what the manifest's names turned out to refer to.
type resolvedManifest struct {
	// model is the pinned network in the solver's SI contract.
	model *solverv1.NetworkModel

	// issuedAt is the pinned cycle's issuance: the knowledge-time axis of
	// every rating lookup this run makes.
	issuedAt time.Time
}

// resolve turns the manifest's pinned names into the things they name, and
// fails naming whichever one drifted.
//
// The model comes from its stored canonical bytes rather than being re-parsed
// from a source file, following apps/bsctl's wireModel: what is solved is
// what the content hash names, so a result citing that hash cites the network
// it was actually computed from.
func (e *Engine) resolve(ctx context.Context, m scn.Manifest) (resolvedManifest, error) {
	found, err := e.collaborator.Models.FindByHash(ctx, m.ModelHash)
	if err != nil {
		return resolvedManifest{}, fmt.Errorf(
			"the manifest pins model %s, which this platform cannot resolve: %w",
			m.ModelHash, err)
	}

	encoded, err := e.collaborator.Models.Encoding(ctx, m.ModelHash)
	if err != nil {
		return resolvedManifest{}, fmt.Errorf(
			"reading the stored encoding of model %s (%s): %w", m.ModelHash, found.Name, err)
	}

	decoded, err := canonical.Decode(encoded)
	if err != nil {
		return resolvedManifest{}, fmt.Errorf(
			"decoding the stored encoding of model %s (%s): %w", m.ModelHash, found.Name, err)
	}

	// The hash is verified against the bytes that came back rather than
	// trusted because the lookup was by hash. The solver already refuses a
	// hash it does not know; this catches the other case — a model re-imported
	// or repaired under the same name, whose row now points at different
	// bytes. Recomputed from the decoded model rather than by hashing the
	// blob, so the round trip is checked at the same time: what is verified is
	// what will be solved.
	//
	// canonical.Hash, not solver.HashOnly. They are different hash spaces —
	// one over the canonical encoding, one over deterministic protobuf — and
	// this is the hash networkmodel stores and the manifest pins. Comparing
	// against the other would fail every run for no reason.
	actual, err := canonical.Hash(decoded)
	if err != nil {
		return resolvedManifest{}, fmt.Errorf("hashing model %s: %w", m.ModelHash, err)
	}

	if actual != m.ModelHash {
		return resolvedManifest{}, fmt.Errorf(
			"the manifest pins model %s (%s) and its stored encoding now hashes to "+
				"%s; the network behind that name has drifted, so this run would not "+
				"be the run the manifest describes", m.ModelHash, found.Name, actual)
	}

	// The stored name, not the hash. It goes into the wire model the solver
	// hashes, so resolving one network under two names would produce two
	// solver-side hashes for one model and miss the cache on every run.
	model, err := wire.Model(found.Name, decoded)
	if err != nil {
		return resolvedManifest{}, fmt.Errorf(
			"converting model %s (%s) to the solver contract: %w",
			m.ModelHash, found.Name, err)
	}

	cycleID, err := wxid.ParseCycle(m.WeatherCycle)
	if err != nil {
		return resolvedManifest{}, fmt.Errorf(
			"the manifest pins weather cycle %q, which is not a cycle identifier: %w",
			m.WeatherCycle, err)
	}

	cycle, err := e.collaborator.Cycles.Cycle(ctx, cycleID)
	if err != nil {
		return resolvedManifest{}, fmt.Errorf(
			"the manifest pins weather cycle %s, which this platform cannot "+
				"resolve: %w", m.WeatherCycle, err)
	}

	return resolvedManifest{model: model, issuedAt: cycle.IssuedAt}, nil
}

// ratingGap reports the failure design §4.2 requires: a run claiming a
// weather binding that the weather never reached.
//
// It fails rather than falling back to nameplate ratings. A cascade computed
// on static ratings while naming a forecast cycle is the most misleading
// result this platform can produce — worse than no answer, because it looks
// like an answer.
//
// Two errors, because there are two causes and they need different fixes. A
// message covering both would send a reader to look at the weather when the
// problem is that their model's branch identifiers do not exist in the asset
// record, which is the more common of the two and the one that has nothing to
// do with weather at all.
func ratingGap(m scn.Manifest, rated Rated) error {
	// A model with no branches is not a weather problem; the screen refuses
	// it a moment later, naming the reason.
	if rated.BranchCount == 0 || rated.WeatherRated > 0 {
		return nil
	}

	if rated.Located == 0 {
		return fmt.Errorf(
			"none of this model's %d branches reach the asset record, so there was "+
				"nothing for cycle %s to rate: no branch identifier resolved to a "+
				"route with spans. That is a join between the model and geo rather "+
				"than a gap in the forecast — a model imported without platform "+
				"branch identifiers produces exactly this, for every branch",
			rated.BranchCount, m.WeatherCycle)
	}

	return fmt.Errorf(
		"cycle %s has computed no ampacity valid at %s for any of the %d branches "+
			"whose spans this model does reach (of %d). The run fails rather than "+
			"falling back to nameplate ratings: a cascade computed on static ratings "+
			"while naming a forecast looks like an answer",
		m.WeatherCycle, m.ValidAt.UTC().Format(time.RFC3339),
		rated.Located, rated.BranchCount)
}
