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

	dsfabriq "github.com/xraph/blackstart/extensions/datasource/store/fabriq"
	"github.com/xraph/blackstart/internal/entities"
	"github.com/xraph/blackstart/internal/testsupport"
)

// The architectural claim, checked rather than asserted.
//
// The project's stated contribution rests on one write producing several
// consistent views. Everything so far has proved the relational half. This
// brings up Postgres, Redis and FalkorDB together, writes through the command
// path, and reads the graph back — because a declared GraphNode with no engine
// behind it is skipped silently by Fabriq, and a declaration nobody checks is
// exactly the kind of claim this project must not make.

// projectionTimeout bounds the wait for the relay to drain the outbox. The
// projection is asynchronous by design — the write commits, the relay
// publishes, the consumer applies — so a test that read immediately would be
// asserting a race rather than a property.
const projectionTimeout = 30 * time.Second

func graphHarness(t *testing.T) (*dsfabriq.Store, *fabriq.Fabriq) {
	t.Helper()

	superDSN := testsupport.Postgres(t)
	testsupport.Migrate(t, superDSN, dsfabriq.Group())

	appDSN := testsupport.AppDSN(t, superDSN)

	reg, err := entities.Build(dsfabriq.Contributor{})
	if err != nil {
		t.Fatalf("building the registry: %v", err)
	}

	fab, stores, err := fabriq.Open(context.Background(), reg, fabriq.Config{
		Postgres: fabriq.PostgresConfig{DSN: appDSN},
		Redis:    fabriq.RedisConfig{Addr: testsupport.Redis(t)},
		FalkorDB: fabriq.FalkorDBConfig{Addr: testsupport.Graph(t)},
		Projections: fabriq.ProjectionsConfig{
			Graph: true,
		},
	})
	if err != nil {
		t.Fatalf("opening fabriq with the graph plane: %v", err)
	}

	t.Cleanup(func() { _ = fab.Close() })

	// The relay and the graph consumer are separate loops that fabriq.Open
	// does not start — a fact this test is what surfaced. Production runs them
	// through the fabric extension's worker; here they are driven directly, so
	// the test exercises the same components without a Forge application.
	startProjection(t, reg, stores)

	s, err := dsfabriq.New(fab, tenantID)
	if err != nil {
		t.Fatalf("building the store: %v", err)
	}

	return s, fab
}

func TestOneWriteProducesBothARowAndAGraphEdge(t *testing.T) {
	s, fab := graphHarness(t)
	ctx := t.Context()

	src := newSource("noaa")

	if err := s.Sources().Create(ctx, src); err != nil {
		t.Fatalf("Create source: %v", err)
	}

	fd := feedFor(src.ID, "hrrr")

	if err := s.Feeds().Create(ctx, fd); err != nil {
		t.Fatalf("Create feed: %v", err)
	}

	// The relational half, which the rest of the suite already covers.
	if _, err := s.Sources().Get(ctx, src.ID); err != nil {
		t.Fatalf("the source is missing from the source of truth: %v", err)
	}

	// The graph half. Both writes went through fabriq.Exec and nothing here
	// wrote to FalkorDB — if the edge is present, the projection produced it.
	scoped := tenant.MustWithTenant(ctx, tenantID)

	waitForGraph(t, fab, scoped,
		`MATCH (f:DataFeed {id: $feed})-[:BELONGS_TO]->(s:DataSource {id: $source})
		 RETURN s.id AS id`,
		map[string]any{"feed": fd.ID.String(), "source": src.ID.String()})
}

// A source with no feeds must still reach the graph as a node. An edge-only
// projection would leave every unreferenced entity invisible, which is the
// failure mode that looks fine until something queries for one.
func TestASourceWithNoFeedsStillReachesTheGraph(t *testing.T) {
	s, fab := graphHarness(t)
	ctx := t.Context()

	src := newSource("lonely")

	if err := s.Sources().Create(ctx, src); err != nil {
		t.Fatalf("Create source: %v", err)
	}

	scoped := tenant.MustWithTenant(ctx, tenantID)

	waitForGraph(t, fab, scoped,
		`MATCH (s:DataSource {id: $source}) RETURN s.id AS id`,
		map[string]any{"source": src.ID.String()})
}

// startProjection runs the outbox relay and the graph consumer for the
// lifetime of the test.
func startProjection(t *testing.T, reg *registry.Registry, stores *fabriq.Stores) {
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
// unpleasant. The failure message says what was being waited for, since "graph
// query returned nothing" alone gives a reader no way to tell a broken
// projection from a broken query.
func waitForGraph(
	t *testing.T, fab *fabriq.Fabriq, ctx context.Context,
	cypher string, params map[string]any,
) {
	t.Helper()

	deadline := time.Now().Add(projectionTimeout)

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
		"Both writes went through fabriq.Exec and nothing wrote to the graph "+
		"directly, so either the relay is not draining the outbox or the entity "+
		"spec's GraphNode is not taking effect.",
		projectionTimeout, cypher, params, lastErr)
}
