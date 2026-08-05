//go:build integration

package fabriq_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/xraph/fabriq"
	"github.com/xraph/fabriq/adapters/postgres"
	"github.com/xraph/fabriq/core/registry"
	"github.com/xraph/fabriq/core/tenant"

	dsid "github.com/xraph/blackstart/extensions/datasource/id"
	"github.com/xraph/blackstart/extensions/datasource/source"
	dsfabriq "github.com/xraph/blackstart/extensions/datasource/store/fabriq"
	pvid "github.com/xraph/blackstart/extensions/provenance/id"
	"github.com/xraph/blackstart/extensions/provenance/record"
	pvfabriq "github.com/xraph/blackstart/extensions/provenance/store/fabriq"
	"github.com/xraph/blackstart/internal/entities"
	"github.com/xraph/blackstart/internal/testsupport"
)

// The claim ADR 0006 makes, checked rather than asserted.
//
// A declared GraphNode with no engine behind it is skipped silently by Fabriq,
// and a declaration nobody checks is exactly the kind of claim this project
// must not make. These tests bring up Postgres, Redis and FalkorDB together,
// write only through the command path, and read the graph back.

// graphTimeout bounds the wait for the relay to drain the outbox. The
// projection is asynchronous by design — the write commits, the relay
// publishes, the consumer applies — so a test that read immediately would be
// asserting a race rather than a property.
const graphTimeout = 30 * time.Second

func graphHarness(t *testing.T) (*pvfabriq.Store, *dsfabriq.Store, *fabriq.Fabriq) {
	t.Helper()

	superDSN := testsupport.Postgres(t)
	testsupport.Migrate(t, superDSN, dsfabriq.Group(), pvfabriq.Group())

	appDSN := testsupport.AppDSN(t, superDSN)

	reg, err := entities.Build(dsfabriq.Contributor{}, pvfabriq.Contributor{})
	if err != nil {
		t.Fatalf("building the registry: %v", err)
	}

	fab, stores, err := fabriq.Open(context.Background(), reg, fabriq.Config{
		Postgres:    fabriq.PostgresConfig{DSN: appDSN},
		Redis:       fabriq.RedisConfig{Addr: testsupport.Redis(t)},
		FalkorDB:    fabriq.FalkorDBConfig{Addr: testsupport.Graph(t)},
		Projections: fabriq.ProjectionsConfig{Graph: true},
	})
	if err != nil {
		t.Fatalf("opening fabriq with the graph plane: %v", err)
	}

	t.Cleanup(func() { _ = fab.Close() })

	startGraphProjection(t, reg, stores)

	pv, err := pvfabriq.New(fab, tenantID)
	if err != nil {
		t.Fatalf("building the provenance store: %v", err)
	}

	ds, err := dsfabriq.New(fab, tenantID)
	if err != nil {
		t.Fatalf("building the datasource store: %v", err)
	}

	return pv, ds, fab
}

// The whole chain, in one traversal.
//
// This is the query Phase 3 could not answer: a weather grid names a dataset
// version, and until now that identifier pointed at nothing that survived a
// restart. Walking run → dataset → source is what "every figure traces to the
// dataset version that fed it" means operationally.
func TestTheProvenanceChainIsOneTraversal(t *testing.T) {
	pv, ds, fab := graphHarness(t)
	ctx := t.Context()

	src := source.Source{
		ID:       dsid.NewSource(),
		Slug:     "noaa",
		Name:     "NOAA",
		Kind:     source.KindWeather,
		Provides: "forecasts",
		License:  "public domain",
	}
	if err := ds.Sources().Create(ctx, src); err != nil {
		t.Fatalf("Create source: %v", err)
	}

	dataset := aDataset(hash)
	dataset.SourceID = src.ID

	if _, err := pv.Records().CreateDataset(ctx, dataset); err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}

	producer := aModel()
	if _, err := pv.Records().CreateModel(ctx, producer); err != nil {
		t.Fatalf("CreateModel: %v", err)
	}

	run := record.ProvenanceRun{
		ID: pvid.NewRun(), ModelVersionID: producer.ID, Kind: "import",
		StartedAt: time.Now().UTC(),
	}
	if _, err := pv.Records().CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	if _, err := pv.Records().CreateLineage(ctx, record.Lineage{
		ID: pvid.NewLineage(), RunID: run.ID, DatasetVersionID: dataset.ID,
		Role: record.RoleInput, RecordedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateLineage: %v", err)
	}

	scoped := tenant.MustWithTenant(ctx, tenantID)

	// Nothing here wrote to FalkorDB. If the path is present, the projection
	// produced it from the same writes that produced the rows.
	waitForGraph(t, fab, scoped,
		`MATCH (r:ProvenanceRun {id: $run})-[:USED]->(d:DatasetVersion {id: $dataset})
		       -[:DERIVED_FROM]->(s:DataSource {id: $source})
		 RETURN s.id AS id`,
		map[string]any{
			"run": run.ID.String(), "dataset": dataset.ID.String(),
			"source": src.ID.String(),
		})

	// The producer half of ADR 0006's chain: what computed it, and therefore
	// from which commit.
	waitForGraph(t, fab, scoped,
		`MATCH (r:ProvenanceRun {id: $run})-[:COMPUTED_BY]->(m:ModelVersion)
		 RETURN m.id AS id`,
		map[string]any{"run": run.ID.String()})
}

// Roles stay distinct in the graph for the same reason they are distinct in
// the domain: a backtest that read training data has leaked, and one edge type
// for all three would hide it behind a traversal that looks clean.
func TestTrainingInputsProjectAsADistinctEdge(t *testing.T) {
	pv, _, fab := graphHarness(t)
	ctx := t.Context()

	producer := aModel()
	if _, err := pv.Records().CreateModel(ctx, producer); err != nil {
		t.Fatalf("CreateModel: %v", err)
	}

	run := record.ProvenanceRun{
		ID: pvid.NewRun(), ModelVersionID: producer.ID, Kind: "fit",
		StartedAt: time.Now().UTC(),
	}
	if _, err := pv.Records().CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	dataset := aDataset(hash)
	if _, err := pv.Records().CreateDataset(ctx, dataset); err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}

	if _, err := pv.Records().CreateLineage(ctx, record.Lineage{
		ID: pvid.NewLineage(), RunID: run.ID, DatasetVersionID: dataset.ID,
		Role: record.RoleTraining, RecordedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateLineage: %v", err)
	}

	scoped := tenant.MustWithTenant(ctx, tenantID)

	waitForGraph(t, fab, scoped,
		`MATCH (r:ProvenanceRun {id: $run})-[:TRAINED_ON]->(d:DatasetVersion)
		 RETURN d.id AS id`,
		map[string]any{"run": run.ID.String()})
}

// startGraphProjection runs the outbox relay and the graph consumer for the
// lifetime of the test.
func startGraphProjection(t *testing.T, reg *registry.Registry, stores *fabriq.Stores) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())

	engine, err := stores.GraphEngine(reg, nil)
	if err != nil {
		cancel()
		t.Fatalf("assembling the graph consumer: %v", err)
	}

	relay := postgres.NewRelay(stores.Postgres, reg, stores.Redis)

	var wg sync.WaitGroup

	wg.Go(func() { _ = relay.Run(ctx) })
	wg.Go(func() { _ = engine.Run(ctx, "test-consumer") })

	t.Cleanup(func() {
		cancel()
		wg.Wait()
	})
}

// waitForGraph polls until the query returns a row or the timeout expires.
//
// Polling rather than sleeping: the projection latency is not fixed, and a
// sleep long enough to be reliable would be long enough to make the suite
// unpleasant.
func waitForGraph(
	t *testing.T, fab *fabriq.Fabriq, ctx context.Context,
	cypher string, params map[string]any,
) {
	t.Helper()

	deadline := time.Now().Add(graphTimeout)

	var lastErr error

	for time.Now().Before(deadline) {
		var ids []string

		err := fab.Graph().Query(ctx, cypher, params, &ids)
		if err == nil && len(ids) > 0 {
			return
		}

		lastErr = err

		time.Sleep(250 * time.Millisecond)
	}

	t.Fatalf("the graph projection did not produce the expected result within %s.\n"+
		"cypher: %s\nparams: %v\nlast error: %v\n"+
		"Every write went through fabriq.Exec and nothing wrote to the graph "+
		"directly, so either the relay is not draining the outbox or the entity "+
		"spec's GraphNode or GraphEdge is not taking effect.",
		graphTimeout, cypher, params, lastErr)
}
