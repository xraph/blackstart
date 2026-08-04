package fabriq

import (
	"testing"
	"time"

	dsid "github.com/xraph/blackstart/extensions/datasource/id"
	pvid "github.com/xraph/blackstart/extensions/provenance/id"
	"github.com/xraph/blackstart/extensions/provenance/record"
)

const hash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// A dataset survives the round trip through its persistence shape.
func TestDatasetRoundTrip(t *testing.T) {
	in := record.DatasetVersion{
		ID:            pvid.NewDataset(),
		SourceID:      dsid.NewSource(),
		URL:           "https://example.gov/hrrr.grib2",
		ContentHash:   hash,
		SizeBytes:     4096,
		ParserVersion: "grib2/0.1.0",
		RetrievedAt:   time.Now().UTC().Truncate(time.Microsecond),
	}

	m, err := datasetToModel(in)
	if err != nil {
		t.Fatalf("datasetToModel: %v", err)
	}

	got, err := datasetFromModel(*m)
	if err != nil {
		t.Fatalf("datasetFromModel: %v", err)
	}

	if got != in {
		t.Errorf("round trip changed the record:\n got %+v\nwant %+v", got, in)
	}
}

// SourceID is optional: DatasetVersion.Validate does not require it, so a
// dataset recorded without a registry entry must not fail to map. The empty
// string is also what tells Fabriq's projector to write no edge.
func TestDatasetWithoutASourceRoundTrips(t *testing.T) {
	in := record.DatasetVersion{
		ID:            pvid.NewDataset(),
		URL:           "https://example.gov/adhoc.csv",
		ContentHash:   hash,
		ParserVersion: "csv/0.1.0",
		RetrievedAt:   time.Now().UTC().Truncate(time.Microsecond),
	}

	m, err := datasetToModel(in)
	if err != nil {
		t.Fatalf("datasetToModel: %v", err)
	}

	if m.SourceID != "" {
		t.Errorf("an absent source became %q; the projector reads empty as no edge", m.SourceID)
	}

	got, err := datasetFromModel(*m)
	if err != nil {
		t.Fatalf("datasetFromModel: %v", err)
	}

	if !got.SourceID.IsZero() {
		t.Errorf("an absent source came back as %s", got.SourceID)
	}
}

// Parameters are JSON in the column and a map in the domain. An empty map and
// a nil map both have to survive, since most producers record neither.
func TestModelParametersRoundTrip(t *testing.T) {
	in := record.ModelVersion{
		ID:         pvid.NewModel(),
		Kind:       record.KindSolver,
		Name:       "bs-solver",
		Version:    "0.4.1",
		CodeCommit: "9f2c1ab",
		Parameters: map[string]string{"tolerance": "1e-6", "max_iter": "40"},
		RecordedAt: time.Now().UTC().Truncate(time.Microsecond),
	}

	m, err := modelToModel(in)
	if err != nil {
		t.Fatalf("modelToModel: %v", err)
	}

	got, err := modelFromModel(*m)
	if err != nil {
		t.Fatalf("modelFromModel: %v", err)
	}

	if got.Version != in.Version {
		t.Errorf("producer version %q became %q", in.Version, got.Version)
	}

	if len(got.Parameters) != 2 || got.Parameters["tolerance"] != "1e-6" {
		t.Errorf("parameters did not survive: %+v", got.Parameters)
	}
}

func TestModelWithNoParametersRoundTrips(t *testing.T) {
	in := record.ModelVersion{
		ID:         pvid.NewModel(),
		Kind:       record.KindParser,
		Name:       "hrrr-parser",
		Version:    "0.1.0",
		CodeCommit: "abc1234",
		RecordedAt: time.Now().UTC().Truncate(time.Microsecond),
	}

	m, err := modelToModel(in)
	if err != nil {
		t.Fatalf("modelToModel: %v", err)
	}

	got, err := modelFromModel(*m)
	if err != nil {
		t.Fatalf("modelFromModel: %v", err)
	}

	if len(got.Parameters) != 0 {
		t.Errorf("absent parameters came back as %+v", got.Parameters)
	}
}

// A run that has not finished carries a zero FinishedAt, which the column
// stores as NULL rather than as a year-1 timestamp that reads like data.
func TestUnfinishedRunRoundTrips(t *testing.T) {
	in := record.Run{
		ID:             pvid.NewRun(),
		ModelVersionID: pvid.NewModel(),
		Kind:           "powerflow",
		StartedAt:      time.Now().UTC().Truncate(time.Microsecond),
	}

	got, err := runFromModel(*runToModel(in))
	if err != nil {
		t.Fatalf("runFromModel: %v", err)
	}

	if !got.FinishedAt.IsZero() {
		t.Errorf("an unfinished run came back finished at %s", got.FinishedAt)
	}

	if got.ID != in.ID || got.ModelVersionID != in.ModelVersionID {
		t.Errorf("round trip changed the identifiers: %+v", got)
	}
}

// The relation column is derived from the role so the two cannot disagree —
// the same denormalise-at-write-time move contingency uses for `concerning`.
func TestRelationIsDerivedFromRole(t *testing.T) {
	cases := map[record.Role]string{
		record.RoleInput:      "USED",
		record.RoleTraining:   "TRAINED_ON",
		record.RoleValidation: "VALIDATED_ON",
	}

	for role, want := range cases {
		edge := record.Lineage{
			ID:               pvid.NewLineage(),
			RunID:            pvid.NewRun(),
			DatasetVersionID: pvid.NewDataset(),
			Role:             role,
			RecordedAt:       time.Now().UTC().Truncate(time.Microsecond),
		}

		m := lineageToModel(edge)

		if m.Relation != want {
			t.Errorf("role %q produced relation %q, want %q", role, m.Relation, want)
		}

		got, err := lineageFromModel(*m)
		if err != nil {
			t.Fatalf("lineageFromModel: %v", err)
		}

		if got.Role != role {
			t.Errorf("role %q came back as %q", role, got.Role)
		}
	}
}
