package hazard

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/blackstart/extensions/provenance"
	"github.com/xraph/blackstart/extensions/provenance/record"
	"github.com/xraph/blackstart/pkg/buildinfo"
	"github.com/xraph/blackstart/pkg/id"
)

// recordRun records the model version and the run, naming the cycle's grids as
// inputs.
//
// Returns the zero identifier when the engine has no recorder. That is a real
// deployment — bsctl computing against a local store — and the computation row
// records the absence rather than inventing a run that nothing could resolve.
func (e *Engine) recordRun(
	ctx context.Context, cycleID id.ID, startedAt time.Time,
) (id.ID, error) {
	if e.recorder == nil {
		return id.Nil, nil
	}

	build := buildinfo.Get()

	model, err := e.recorder.RecordModel(ctx, record.ModelVersion{
		Kind:       record.KindModel,
		Name:       ModelName,
		Version:    ModelVersion,
		CodeCommit: build.Commit,
		RecordedAt: e.now().UTC(),
	})
	if err != nil {
		return id.Nil, fmt.Errorf("hazard: recording the model version: %w", err)
	}

	// The inputs are the dataset versions of the weather grids this pass read.
	// The Grid rows already carry them, so the link is a lookup rather than an
	// invention — which is the whole reason the weather extension records one
	// per retrieval.
	inputs, err := e.runInputs(ctx, cycleID)
	if err != nil {
		return id.Nil, err
	}

	run, err := e.recorder.RecordRun(ctx, record.ProvenanceRun{
		ModelVersionID: model.ID,
		Kind:           "hazard",
		StartedAt:      startedAt,
		FinishedAt:     e.now().UTC(),
	}, inputs)
	if err != nil {
		return id.Nil, fmt.Errorf("hazard: recording the run: %w", err)
	}

	return run.ID, nil
}

// runInputs collects the dataset versions behind a cycle's grids, deduplicated.
func (e *Engine) runInputs(
	ctx context.Context, cycleID id.ID,
) ([]provenance.Input, error) {
	grids, err := e.weather.Grids(ctx, cycleID)
	if err != nil {
		return nil, fmt.Errorf(
			"hazard: listing the grids of cycle %s: %w", cycleID, err)
	}

	seen := map[id.ID]bool{}
	out := make([]provenance.Input, 0, len(grids))

	for _, grid := range grids {
		if grid.DatasetVersionID.IsZero() || seen[grid.DatasetVersionID] {
			continue
		}

		seen[grid.DatasetVersionID] = true

		out = append(out, provenance.Input{
			DatasetVersionID: grid.DatasetVersionID,
			Role:             record.RoleInput,
		})
	}

	return out, nil
}
