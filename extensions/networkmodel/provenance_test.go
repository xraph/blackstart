package networkmodel_test

import (
	"context"
	"errors"
	"testing"

	"github.com/xraph/blackstart/extensions/networkmodel"
	"github.com/xraph/blackstart/extensions/provenance"
	"github.com/xraph/blackstart/extensions/provenance/record"
)

// ADR 0006's chain has to be established here, because this is the one place
// that turns a file into something the solver can be asked about.
func TestAnImportRecordsWhereTheBytesCameFrom(t *testing.T) {
	provenanceEngine := recorder(t)
	e := engine(t, provenanceEngine, nil)

	got, err := e.Import(t.Context(), networkmodel.ImportRequest{
		Source: []byte(twoBus),
		URL:    "https://example.invalid/case.m",
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if got.DatasetVersionID.IsZero() {
		t.Fatal("the import recorded no dataset version")
	}

	dataset, err := provenanceEngine.GetDataset(t.Context(), got.DatasetVersionID)
	if err != nil {
		t.Fatalf("the recorded dataset version cannot be read back: %v", err)
	}

	if dataset.URL != "https://example.invalid/case.m" {
		t.Errorf("the record says the bytes came from %q", dataset.URL)
	}

	if dataset.ParserVersion == "" {
		t.Error("the record does not name the parser that read the bytes")
	}

	if dataset.SizeBytes != int64(len(twoBus)) {
		t.Errorf("the record says %d bytes, want %d", dataset.SizeBytes, len(twoBus))
	}
}

// The dataset version records that someone fetched a file, which is a fact
// about the world even when the file turns out to be one already known.
func TestAReimportStillRecordsTheRetrieval(t *testing.T) {
	provenanceEngine := recorder(t)
	e := engine(t, provenanceEngine, nil)

	if _, err := e.Import(t.Context(), networkmodel.ImportRequest{
		Source: []byte(twoBus), URL: "file:///a.m",
	}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	second, err := e.Import(t.Context(), networkmodel.ImportRequest{
		Source: []byte(twoBus), URL: "file:///a.m",
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if second.DatasetVersionID.IsZero() {
		t.Error("a re-import recorded no retrieval")
	}
}

// A provenance store being down must not stop a model being built — but a
// model built without a chain must not claim to have one.
func TestAProvenanceFailureIsSurvivableAndReported(t *testing.T) {
	e := engine(t, failingRecorder{}, nil)

	got, err := e.Import(t.Context(), networkmodel.ImportRequest{Source: []byte(twoBus)})
	if err != nil {
		t.Fatalf("a provenance failure stopped the import: %v", err)
	}

	if got.Model.ID.IsZero() {
		t.Error("no model was built")
	}

	if got.Traceable {
		t.Error("a model built with no provenance claimed to be traceable")
	}

	if !got.DatasetVersionID.IsZero() {
		t.Error("a failed recording still produced a dataset identifier")
	}
}

// bsctl run against a local file has no provenance store, and refusing to work
// without one would make the tool unusable for the exploration it exists for.
func TestNoRecorderIsAllowedAndSaysSo(t *testing.T) {
	e := engine(t, nil, nil)

	got, err := e.Import(t.Context(), networkmodel.ImportRequest{Source: []byte(twoBus)})
	if err != nil {
		t.Fatalf("Import with no recorder: %v", err)
	}

	if got.Traceable {
		t.Error("a model built with no recorder claimed to be traceable")
	}
}

// failingRecorder stands in for a provenance store that is down.
type failingRecorder struct{}

func (failingRecorder) RecordDataset(
	context.Context, record.DatasetVersion,
) (record.DatasetVersion, error) {
	return record.DatasetVersion{}, errors.New("the provenance store is unreachable")
}

func (failingRecorder) RecordModel(
	context.Context, record.ModelVersion,
) (record.ModelVersion, error) {
	return record.ModelVersion{}, errors.New("the provenance store is unreachable")
}

func (failingRecorder) RecordRun(
	context.Context, record.ProvenanceRun, []provenance.Input,
) (record.ProvenanceRun, error) {
	return record.ProvenanceRun{}, errors.New("the provenance store is unreachable")
}
