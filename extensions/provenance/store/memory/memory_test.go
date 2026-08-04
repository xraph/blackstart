package memory_test

import (
	"testing"
	"time"

	pvid "github.com/xraph/blackstart/extensions/provenance/id"
	"github.com/xraph/blackstart/extensions/provenance/record"
	"github.com/xraph/blackstart/extensions/provenance/store/memory"
)

const hash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// The in-memory store is what the engine is tested against, so it has to
// behave like the Fabriq one in the ways the engine depends on — including
// this one. Silently keeping two records for identical bytes would make the
// engine's dedupe look correct in tests and race in production.
func TestCreateDatasetIsIdempotentOnContentHash(t *testing.T) {
	s := memory.New()
	ctx := t.Context()

	first := record.DatasetVersion{
		ID:            pvid.NewDataset(),
		URL:           "https://example.gov/hrrr.grib2",
		ContentHash:   hash,
		ParserVersion: "grib2/0.1.0",
		RetrievedAt:   time.Now().UTC(),
	}

	if _, err := s.Records().CreateDataset(ctx, first); err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}

	second := first
	second.ID = pvid.NewDataset()
	second.URL = "https://mirror.example/hrrr.grib2"

	got, err := s.Records().CreateDataset(ctx, second)
	if err != nil {
		t.Fatalf("the second retrieval of identical bytes failed: %v", err)
	}

	if got.ID != first.ID {
		t.Errorf("the second retrieval created version %s; the first was %s", got.ID, first.ID)
	}

	found, err := s.Records().FindDatasetByHash(ctx, hash)
	if err != nil {
		t.Fatalf("FindDatasetByHash: %v", err)
	}

	if found.URL != first.URL {
		t.Errorf("the hash index points at the second record: %+v", found)
	}
}
