//go:build integration

package fabriq_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xraph/fabriq"

	dsfabriq "github.com/xraph/blackstart/extensions/datasource/store/fabriq"
	pvid "github.com/xraph/blackstart/extensions/provenance/id"
	"github.com/xraph/blackstart/extensions/provenance/record"
	pvfabriq "github.com/xraph/blackstart/extensions/provenance/store/fabriq"
	"github.com/xraph/blackstart/internal/entities"
	"github.com/xraph/blackstart/internal/testsupport"
	"github.com/xraph/blackstart/pkg/bserr"
)

// harness brings up Postgres, runs both migration groups through one
// orchestrator, provisions the restricted application role, and opens Fabriq.
//
// The datasource group comes along because a dataset's source_id edge targets
// datasource_source, and the registry refuses an edge whose target it has
// never heard of.
func harness(t *testing.T) *pvfabriq.Store {
	t.Helper()

	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, dsfabriq.Group(), pvfabriq.Group())

	// The application connects as a non-superuser, because PostgreSQL row
	// security never constrains a superuser — a test that connected as one
	// would pass every isolation check while proving nothing.
	appDSN := testsupport.AppDSN(t, superDSN)

	reg, err := entities.Build(dsfabriq.Contributor{}, pvfabriq.Contributor{})
	if err != nil {
		t.Fatalf("building the registry: %v", err)
	}

	fab, _, err := fabriq.Open(context.Background(), reg, fabriq.Config{
		Postgres: fabriq.PostgresConfig{DSN: appDSN},
	})
	if err != nil {
		t.Fatalf("opening fabriq: %v", err)
	}

	t.Cleanup(func() { _ = fab.Close() })

	s, err := pvfabriq.New(fab, tenantID)
	if err != nil {
		t.Fatalf("building the store: %v", err)
	}

	return s
}

func aDataset(contentHash string) record.DatasetVersion {
	return record.DatasetVersion{
		ID:            pvid.NewDataset(),
		URL:           "https://example.gov/hrrr.grib2",
		ContentHash:   contentHash,
		SizeBytes:     4096,
		ParserVersion: "grib2/0.1.0",
		RetrievedAt:   time.Now().UTC().Truncate(time.Microsecond),
	}
}

func aModel() record.ModelVersion {
	return record.ModelVersion{
		ID:         pvid.NewModel(),
		Kind:       record.KindSolver,
		Name:       "bs-solver",
		Version:    "0.4.1",
		CodeCommit: "9f2c1ab",
		Parameters: map[string]string{"tolerance": "1e-6"},
		RecordedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
}

// The record survives the round trip as a typed identifier, not as a bare
// string: Fabriq mints a ULID when AggID is empty, and the store exists partly
// to stop that happening.
func TestDatasetRoundTripsThroughFabriq(t *testing.T) {
	s := harness(t)
	ctx := t.Context()

	in := aDataset(hash)

	stored, err := s.Records().CreateDataset(ctx, in)
	if err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}

	if stored.ID != in.ID {
		t.Errorf("CreateDataset returned identifier %s, want %s", stored.ID, in.ID)
	}

	got, err := s.Records().GetDataset(ctx, in.ID)
	if err != nil {
		t.Fatalf("GetDataset: %v", err)
	}

	if got.ContentHash != in.ContentHash || got.ParserVersion != in.ParserVersion {
		t.Errorf("read back %+v, want %+v", got, in)
	}

	if got.ID.Prefix() != pvid.Dataset {
		t.Errorf("the identifier lost its type: %s", got.ID)
	}

	if !got.RetrievedAt.Equal(in.RetrievedAt) {
		t.Errorf("retrieved at %s, want %s", got.RetrievedAt, in.RetrievedAt)
	}
}

// The question every ingest asks first, and the reason a re-download that
// changed nothing is cheap: it produces no new version, so it invalidates
// nothing derived from the previous one.
func TestFindDatasetByHash(t *testing.T) {
	s := harness(t)
	ctx := t.Context()

	in := aDataset(hash)

	if _, err := s.Records().CreateDataset(ctx, in); err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}

	got, err := s.Records().FindDatasetByHash(ctx, hash)
	if err != nil {
		t.Fatalf("FindDatasetByHash: %v", err)
	}

	if got.ID != in.ID {
		t.Errorf("FindDatasetByHash returned %s, want %s", got.ID, in.ID)
	}
}

func TestModelAndRunRoundTrip(t *testing.T) {
	s := harness(t)
	ctx := t.Context()

	producer := aModel()

	if _, err := s.Records().CreateModel(ctx, producer); err != nil {
		t.Fatalf("CreateModel: %v", err)
	}

	gotModel, err := s.Records().GetModel(ctx, producer.ID)
	if err != nil {
		t.Fatalf("GetModel: %v", err)
	}

	if gotModel.Version != producer.Version || gotModel.CodeCommit != producer.CodeCommit {
		t.Errorf("read back %+v, want %+v", gotModel, producer)
	}

	if gotModel.Parameters["tolerance"] != "1e-6" {
		t.Errorf("parameters did not survive: %+v", gotModel.Parameters)
	}

	run := record.ProvenanceRun{
		ID:             pvid.NewRun(),
		ModelVersionID: producer.ID,
		Kind:           "powerflow",
		ResultHash:     hash,
		StartedAt:      time.Now().UTC().Truncate(time.Microsecond),
		FinishedAt:     time.Now().UTC().Truncate(time.Microsecond),
	}

	if _, err := s.Records().CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	gotRun, err := s.Records().GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}

	if gotRun.ModelVersionID != producer.ID || gotRun.Kind != "powerflow" {
		t.Errorf("read back %+v, want %+v", gotRun, run)
	}
}

// A run that has not finished stores NULL rather than a year-1 timestamp, and
// has to read back as absence rather than as a date in antiquity.
func TestAnUnfinishedRunSurvivesTheDatabase(t *testing.T) {
	s := harness(t)
	ctx := t.Context()

	producer := aModel()
	if _, err := s.Records().CreateModel(ctx, producer); err != nil {
		t.Fatalf("CreateModel: %v", err)
	}

	run := record.ProvenanceRun{
		ID:             pvid.NewRun(),
		ModelVersionID: producer.ID,
		Kind:           "contingency",
		StartedAt:      time.Now().UTC().Truncate(time.Microsecond),
	}

	if _, err := s.Records().CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	got, err := s.Records().GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}

	if !got.FinishedAt.IsZero() {
		t.Errorf("an unfinished run came back finished at %s", got.FinishedAt)
	}
}

// LineageOf answers "what fed this number" in one query rather than a graph
// walk the caller writes — and it answers it for one run, not for whatever
// else happens to be in the table.
func TestLineageOfReturnsOneRunsInputs(t *testing.T) {
	s := harness(t)
	ctx := t.Context()

	producer := aModel()
	if _, err := s.Records().CreateModel(ctx, producer); err != nil {
		t.Fatalf("CreateModel: %v", err)
	}

	mine := record.ProvenanceRun{
		ID: pvid.NewRun(), ModelVersionID: producer.ID, Kind: "import",
		StartedAt: time.Now().UTC(),
	}
	theirs := record.ProvenanceRun{
		ID: pvid.NewRun(), ModelVersionID: producer.ID, Kind: "import",
		StartedAt: time.Now().UTC(),
	}

	for _, r := range []record.ProvenanceRun{mine, theirs} {
		if _, err := s.Records().CreateRun(ctx, r); err != nil {
			t.Fatalf("CreateRun: %v", err)
		}
	}

	first := aDataset(hash)
	second := aDataset("a" + hash[1:])

	for _, d := range []record.DatasetVersion{first, second} {
		if _, err := s.Records().CreateDataset(ctx, d); err != nil {
			t.Fatalf("CreateDataset: %v", err)
		}
	}

	edges := []record.Lineage{
		{
			ID: pvid.NewLineage(), RunID: mine.ID, DatasetVersionID: first.ID,
			Role: record.RoleTraining, RecordedAt: time.Now().UTC(),
		},
		{
			ID: pvid.NewLineage(), RunID: mine.ID, DatasetVersionID: second.ID,
			Role: record.RoleValidation, RecordedAt: time.Now().UTC(),
		},
		{
			ID: pvid.NewLineage(), RunID: theirs.ID, DatasetVersionID: first.ID,
			Role: record.RoleInput, RecordedAt: time.Now().UTC(),
		},
	}

	for _, e := range edges {
		if _, err := s.Records().CreateLineage(ctx, e); err != nil {
			t.Fatalf("CreateLineage: %v", err)
		}
	}

	got, err := s.Records().LineageOf(ctx, mine.ID)
	if err != nil {
		t.Fatalf("LineageOf: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("LineageOf returned %d edges, want 2 (the other run's must not appear)", len(got))
	}

	// Roles are distinguished because a backtest reading training data has
	// leaked, so they must come back as they went in rather than collapsing to
	// a default.
	roles := map[record.Role]bool{}

	for _, e := range got {
		roles[e.Role] = true

		if e.RunID != mine.ID {
			t.Errorf("edge %s belongs to run %s", e.ID, e.RunID)
		}
	}

	if !roles[record.RoleTraining] || !roles[record.RoleValidation] {
		t.Errorf("roles did not survive: %+v", roles)
	}
}

func TestPing(t *testing.T) {
	s := harness(t)

	if err := s.Ping(t.Context()); err != nil {
		t.Errorf("Ping: %v", err)
	}

	if s.StoreName() != "provenance/fabriq" {
		t.Errorf("StoreName is %q", s.StoreName())
	}
}

// Recording the same bytes twice yields one version, not two. The unique index
// is the authority — two concurrent ingests would both pass a read-then-write
// check in the engine — so the store absorbs the collision and returns what is
// already there, which is what Engine.RecordDataset already promises.
func TestRecordingTheSameBytesTwiceYieldsOneVersion(t *testing.T) {
	s := harness(t)
	ctx := t.Context()

	first := aDataset(hash)

	if _, err := s.Records().CreateDataset(ctx, first); err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}

	// A different identifier and a different URL, the same bytes. A mirror
	// serving an identical file is exactly this case.
	second := aDataset(hash)
	second.URL = "https://mirror.example/hrrr.grib2"

	got, err := s.Records().CreateDataset(ctx, second)
	if err != nil {
		t.Fatalf("the second retrieval of identical bytes failed: %v", err)
	}

	if got.ID != first.ID {
		t.Errorf("the second retrieval created version %s; the first was %s", got.ID, first.ID)
	}

	if got.URL != first.URL {
		t.Errorf("the returned record is the second one, not the stored first: %+v", got)
	}
}

// A run naming a producer that was never recorded would be a broken chain, and
// a broken chain is worse than a missing one: it looks traceable. The database
// refuses it; the store must report it the way the in-memory store does, or
// the engine's tests describe behaviour the deployment does not have.
func TestARunCannotNameAnUnrecordedModelVersion(t *testing.T) {
	s := harness(t)
	ctx := t.Context()

	orphan := record.ProvenanceRun{
		ID:             pvid.NewRun(),
		ModelVersionID: pvid.NewModel(),
		Kind:           "powerflow",
		StartedAt:      time.Now().UTC(),
	}

	_, err := s.Records().CreateRun(ctx, orphan)
	if err == nil {
		t.Fatal("a run naming an unrecorded model version was accepted")
	}

	if !errors.Is(err, bserr.ErrInvalid) {
		t.Errorf("want ErrInvalid, matching the in-memory store, got %v", err)
	}

	if !strings.Contains(err.Error(), "never recorded") {
		t.Errorf("the message should say what is missing, got %v", err)
	}
}

func TestALineageEdgeCannotNameAnUnrecordedEnd(t *testing.T) {
	s := harness(t)
	ctx := t.Context()

	producer := aModel()
	if _, err := s.Records().CreateModel(ctx, producer); err != nil {
		t.Fatalf("CreateModel: %v", err)
	}

	run := record.ProvenanceRun{
		ID: pvid.NewRun(), ModelVersionID: producer.ID, Kind: "import",
		StartedAt: time.Now().UTC(),
	}
	if _, err := s.Records().CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	// The run exists; the dataset does not.
	_, err := s.Records().CreateLineage(ctx, record.Lineage{
		ID:               pvid.NewLineage(),
		RunID:            run.ID,
		DatasetVersionID: pvid.NewDataset(),
		Role:             record.RoleInput,
		RecordedAt:       time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("a lineage edge naming an unrecorded dataset version was accepted")
	}

	if !errors.Is(err, bserr.ErrInvalid) {
		t.Errorf("want ErrInvalid, matching the in-memory store, got %v", err)
	}
}
