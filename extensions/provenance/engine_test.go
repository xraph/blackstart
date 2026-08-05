package provenance_test

import (
	"testing"
	"time"

	"github.com/xraph/blackstart/extensions/provenance"
	"github.com/xraph/blackstart/extensions/provenance/record"
	"github.com/xraph/blackstart/extensions/provenance/store/memory"
	"github.com/xraph/blackstart/pkg/hooks"
)

const (
	hashA = "3b1f5c8e2a7d4b6091c3e5f70a2d4b6c8e0f1a3c5d7e9b0f2a4c6e8d0b2f4a6c"
	hashB = "9e2d4b6c8e0f1a3c5d7e9b0f2a4c6e8d0b2f4a6c3b1f5c8e2a7d4b6091c3e5f7"
)

func engine(t *testing.T, bus *hooks.Bus) *provenance.Engine {
	t.Helper()

	e, err := provenance.NewEngine(memory.New(), bus)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	return e
}

func dataset(hash string) record.DatasetVersion {
	return record.DatasetVersion{
		URL:           "https://example.invalid/hrrr.grib2",
		ContentHash:   hash,
		SizeBytes:     1024,
		ParserVersion: "hrrr-parser/0.1.0",
	}
}

func producer() record.ModelVersion {
	return record.ModelVersion{
		Kind:       record.KindSolver,
		Name:       "bs-solver",
		Version:    "0.1.0",
		CodeCommit: "abc123def456",
	}
}

func TestAnEngineNeedsAStore(t *testing.T) {
	if _, err := provenance.NewEngine(nil, nil); err == nil {
		t.Error("NewEngine accepted a nil store")
	}
}

// The same bytes are one version, whatever the timestamps say. That is what
// makes a re-download that changed nothing cheap: there is no new version, so
// nothing derived from the previous one is invalidated.
func TestTheSameBytesAreOneVersion(t *testing.T) {
	e := engine(t, nil)

	first, err := e.RecordDataset(t.Context(), dataset(hashA))
	if err != nil {
		t.Fatalf("RecordDataset: %v", err)
	}

	// A second retrieval of identical bytes, minutes later.
	again := dataset(hashA)
	again.RetrievedAt = time.Now().Add(time.Hour)

	second, err := e.RecordDataset(t.Context(), again)
	if err != nil {
		t.Fatalf("RecordDataset: %v", err)
	}

	if second.ID != first.ID {
		t.Errorf("identical bytes produced two versions: %s and %s", first.ID, second.ID)
	}
}

// Different bytes are different versions, even when the publisher calls them
// the same release.
func TestDifferentBytesAreDifferentVersions(t *testing.T) {
	e := engine(t, nil)

	first, err := e.RecordDataset(t.Context(), dataset(hashA))
	if err != nil {
		t.Fatalf("RecordDataset: %v", err)
	}

	second, err := e.RecordDataset(t.Context(), dataset(hashB))
	if err != nil {
		t.Fatalf("RecordDataset: %v", err)
	}

	if second.ID == first.ID {
		t.Error("different bytes were recorded as one version")
	}
}

// A run recorded without its inputs is worse than no record: it looks
// traceable and is not. Trace is the one call that answers "what produced
// this", which is the question ADR 0006 exists to make mechanical.
func TestARunTracesBackToItsInputsAndItsCommit(t *testing.T) {
	e := engine(t, nil)

	data, err := e.RecordDataset(t.Context(), dataset(hashA))
	if err != nil {
		t.Fatalf("RecordDataset: %v", err)
	}

	solver, err := e.RecordModel(t.Context(), producer())
	if err != nil {
		t.Fatalf("RecordModel: %v", err)
	}

	run, err := e.RecordRun(t.Context(),
		record.ProvenanceRun{ModelVersionID: solver.ID, Kind: "powerflow"},
		[]provenance.Input{{DatasetVersionID: data.ID, Role: record.RoleInput}},
	)
	if err != nil {
		t.Fatalf("RecordRun: %v", err)
	}

	trace, err := e.Trace(t.Context(), run.ID)
	if err != nil {
		t.Fatalf("Trace: %v", err)
	}

	if trace.Producer.CodeCommit != "abc123def456" {
		t.Errorf("the trace names commit %q", trace.Producer.CodeCommit)
	}

	if len(trace.Inputs) != 1 {
		t.Fatalf("the trace names %d inputs, want one", len(trace.Inputs))
	}

	if trace.Inputs[0].Dataset.ContentHash != hashA {
		t.Errorf("the traced input has hash %q", trace.Inputs[0].Dataset.ContentHash)
	}

	if !trace.Reproducible() {
		t.Error("a run with a commit and inputs was reported as irreproducible")
	}
}

// Input, training and validation are separate because a backtest that reads
// training data has leaked, and one word for all three would make that
// undetectable. The role has to survive into the trace or the distinction
// buys nothing.
func TestTheRoleOfEachInputSurvivesIntoTheTrace(t *testing.T) {
	e := engine(t, nil)

	training, err := e.RecordDataset(t.Context(), dataset(hashA))
	if err != nil {
		t.Fatalf("RecordDataset: %v", err)
	}

	validation, err := e.RecordDataset(t.Context(), dataset(hashB))
	if err != nil {
		t.Fatalf("RecordDataset: %v", err)
	}

	solver, err := e.RecordModel(t.Context(), producer())
	if err != nil {
		t.Fatalf("RecordModel: %v", err)
	}

	run, err := e.RecordRun(t.Context(),
		record.ProvenanceRun{ModelVersionID: solver.ID, Kind: "backtest"},
		[]provenance.Input{
			{DatasetVersionID: training.ID, Role: record.RoleTraining},
			{DatasetVersionID: validation.ID, Role: record.RoleValidation},
		},
	)
	if err != nil {
		t.Fatalf("RecordRun: %v", err)
	}

	trace, err := e.Trace(t.Context(), run.ID)
	if err != nil {
		t.Fatalf("Trace: %v", err)
	}

	roles := map[record.Role]string{}
	for _, input := range trace.Inputs {
		roles[input.Role] = input.Dataset.ContentHash
	}

	if roles[record.RoleTraining] != hashA {
		t.Errorf("the training input is %q", roles[record.RoleTraining])
	}

	if roles[record.RoleValidation] != hashB {
		t.Errorf("the validation input is %q", roles[record.RoleValidation])
	}
}
