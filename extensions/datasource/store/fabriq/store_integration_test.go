//go:build integration

package fabriq_test

import (
	"context"
	"errors"
	"testing"

	"github.com/xraph/fabriq"

	dsid "github.com/xraph/blackstart/extensions/datasource/id"
	"github.com/xraph/blackstart/extensions/datasource/source"
	dsfabriq "github.com/xraph/blackstart/extensions/datasource/store/fabriq"
	"github.com/xraph/blackstart/internal/entities"
	"github.com/xraph/blackstart/internal/testsupport"
	"github.com/xraph/blackstart/pkg/store"
)

const tenantID = "blackstart"

// harness brings up Postgres, runs both migration groups through one
// orchestrator, provisions the restricted application role, and opens Fabriq.
//
// One orchestrator matters: grove resolves DependsOn topologically only when
// every group is handed to the same one. Running each extension's separately
// ignores the declaration silently, which its own source notes.
func harness(t *testing.T) *dsfabriq.Store {
	t.Helper()

	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, dsfabriq.Group())

	// The application connects as a non-superuser, because PostgreSQL row
	// security never constrains a superuser — a test that connected as one
	// would pass every isolation check while proving nothing.
	appDSN := testsupport.AppDSN(t, superDSN)

	reg, err := entities.Build(dsfabriq.Contributor{})
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

	s, err := dsfabriq.New(fab, tenantID)
	if err != nil {
		t.Fatalf("building the store: %v", err)
	}

	return s
}

func newSource(slug string) source.Source {
	return source.Source{
		ID:       dsid.NewSource(),
		Slug:     slug,
		Name:     "Source " + slug,
		Kind:     source.KindWeather,
		Provides: "test data",
		License:  "public domain",
	}
}

func TestWriteAndReadBackThroughFabriq(t *testing.T) {
	s := harness(t)
	ctx := t.Context()

	in := newSource("noaa-hrrr")

	if err := s.Sources().Create(ctx, in); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Sources().Get(ctx, in.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Slug != in.Slug || got.License != in.License {
		t.Errorf("read back %+v, want %+v", got, in)
	}

	// The identifier must survive the round trip as a typed identifier, not as
	// a bare string: Fabriq mints a ULID when AggID is empty, and the store
	// exists partly to stop that happening.
	if got.ID != in.ID {
		t.Errorf("identifier changed in the round trip: %s -> %s", in.ID, got.ID)
	}

	if got.ID.Prefix() != dsid.Source {
		t.Errorf("the identifier lost its type: %s", got.ID)
	}
}

// The unique index is the authority on slug collisions, not a read-then-write
// check in the store — two concurrent creates would both pass that.
func TestTheDatabaseEnforcesSlugUniqueness(t *testing.T) {
	s := harness(t)
	ctx := t.Context()

	if err := s.Sources().Create(ctx, newSource("duplicate")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	err := s.Sources().Create(ctx, newSource("duplicate"))
	if err == nil {
		t.Fatal("the database accepted two sources with the same slug")
	}

	if !errors.Is(err, source.ErrSlugTaken) {
		t.Errorf("the collision should surface as ErrSlugTaken, got %v", err)
	}
}

func TestListFiltersAndPaginates(t *testing.T) {
	s := harness(t)
	ctx := t.Context()

	for _, slug := range []string{"a-one", "b-two", "c-three", "d-four"} {
		if err := s.Sources().Create(ctx, newSource(slug)); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	network := newSource("e-network")
	network.Kind = source.KindNetwork

	if err := s.Sources().Create(ctx, network); err != nil {
		t.Fatalf("Create: %v", err)
	}

	byKind, err := s.Sources().List(ctx, source.Filter{Kind: source.KindNetwork}, store.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(byKind) != 1 || byKind[0].Slug != "e-network" {
		t.Errorf("filtering by kind returned %d rows", len(byKind))
	}

	first, err := s.Sources().List(ctx, source.Filter{}, store.Page{Limit: 2})
	if err != nil {
		t.Fatalf("List page one: %v", err)
	}

	if len(first) != 2 {
		t.Fatalf("page one has %d rows, want 2", len(first))
	}

	second, err := s.Sources().List(ctx, source.Filter{},
		store.Page{Limit: 2, After: first[1].ID})
	if err != nil {
		t.Fatalf("List page two: %v", err)
	}

	if len(second) == 0 {
		t.Fatal("page two is empty")
	}

	if second[0].ID.String() <= first[1].ID.String() {
		t.Error("the cursor did not advance; pages overlap")
	}
}

func TestGetBySlug(t *testing.T) {
	s := harness(t)
	ctx := t.Context()

	in := newSource("eagle-i")
	if err := s.Sources().Create(ctx, in); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Sources().GetBySlug(ctx, "eagle-i")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}

	if got.ID != in.ID {
		t.Errorf("GetBySlug returned %s, want %s", got.ID, in.ID)
	}

	if _, err := s.Sources().GetBySlug(ctx, "absent"); !errors.Is(err, source.ErrNotFound) {
		t.Errorf("a missing slug should be not-found, got %v", err)
	}
}

func TestUpdate(t *testing.T) {
	s := harness(t)
	ctx := t.Context()

	in := newSource("eia-860")
	if err := s.Sources().Create(ctx, in); err != nil {
		t.Fatalf("Create: %v", err)
	}

	in.License = "amended terms"
	if err := s.Sources().Update(ctx, in); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := s.Sources().Get(ctx, in.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.License != "amended terms" {
		t.Errorf("the update did not take: %q", got.License)
	}
}

// A migration that cannot be rolled back is one nobody can test safely, which
// is why the project requires both directions to run cleanly. Migrating up
// again afterwards proves the down did not leave the schema in a state the up
// cannot start from — the failure mode a one-way test misses.
func TestMigrationsRunBothWays(t *testing.T) {
	dsn := testsupport.Postgres(t)

	testsupport.Migrate(t, dsn, dsfabriq.Group())

	if err := testsupport.Rollback(t, dsn, dsfabriq.Group()); err != nil {
		t.Fatalf("rolling back: %v", err)
	}

	testsupport.Migrate(t, dsn, dsfabriq.Group())
}
