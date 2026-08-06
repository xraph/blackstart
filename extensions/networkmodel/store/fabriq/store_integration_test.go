//go:build integration

package fabriq_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/xraph/fabriq"

	nmid "github.com/xraph/blackstart/extensions/networkmodel/id"
	"github.com/xraph/blackstart/extensions/networkmodel/model"
	nmfabriq "github.com/xraph/blackstart/extensions/networkmodel/store/fabriq"
	"github.com/xraph/blackstart/internal/entities"
	"github.com/xraph/blackstart/internal/testsupport"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

const tenantID = "blackstart"

// openFabriq brings up Postgres, runs this extension's migration group,
// provisions the restricted application role, and opens Fabriq.
//
// The application connects as a non-superuser, because PostgreSQL row
// security never constrains a superuser — a test that connected as one would
// pass every isolation check while proving nothing, and FORCE ROW LEVEL
// SECURITY on migration 0004's bus table is exactly the property this suite
// exists to exercise.
//
// Only this extension's group: nothing in Contributor.Specs declares an edge
// to another extension's entity, so the registry has nothing else to resolve
// against.
func openFabriq(t *testing.T) *fabriq.Fabriq {
	t.Helper()

	superDSN := testsupport.Postgres(t)

	testsupport.Migrate(t, superDSN, nmfabriq.Group())

	appDSN := testsupport.AppDSN(t, superDSN)

	reg, err := entities.Build(nmfabriq.Contributor{})
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

	return fab
}

// harness is openFabriq plus a store scoped to the suite's one tenant, which
// is what every test below wants except the isolation test, which needs two.
func harness(t *testing.T) *nmfabriq.Store {
	t.Helper()

	return storeFor(t, openFabriq(t), tenantID)
}

func storeFor(t *testing.T, fab *fabriq.Fabriq, tenant string) *nmfabriq.Store {
	t.Helper()

	s, err := nmfabriq.New(fab, tenant)
	if err != nil {
		t.Fatalf("building the store: %v", err)
	}

	return s
}

// aModel builds a storable network model naming no buses or branches of its
// own; the tests that need rows write them separately through Buses().PutAll
// or Branches().PutAll, exactly as an import would.
func aModel(hash string) model.NetworkModel {
	return model.NetworkModel{
		ID:          nmid.NewModel(),
		ContentHash: hash,
		BlobRef:     "sha256:" + hash,
		Name:        "integration test model",
		SourceKind:  "matpower",
		BusCount:    1,
		BranchCount: 1,
		BuiltAt:     time.Now().UTC(),
	}
}

// hashFor mints a distinct, valid content hash per test so models created by
// different tests never collide on the (non-unique, but still real) hash
// index.
func hashFor(name string) string {
	sum := sha256.Sum256([]byte(name))

	return hex.EncodeToString(sum[:])
}

func aBus(modelID id.ID, ordinal string, vNominalV float64) model.Bus {
	return model.Bus{
		ID:        nmid.NewBus(),
		ModelID:   modelID,
		Ordinal:   ordinal,
		VNominalV: vNominalV,
	}
}

func aBranch(modelID id.ID, ordinal, fromBus, toBus string) model.Branch {
	return model.Branch{
		ID:      nmid.NewBranch(),
		ModelID: modelID,
		Ordinal: ordinal,
		FromBus: fromBus,
		ToBus:   toBus,
	}
}

// Bus rows round-trip through PutAll, ForModel and ByOrdinal with VNominalV
// preserved — including the bus whose VNominalV is zero, which is the case
// that only fails against a real database: busRow.VNominalV carries no
// fabriq `notnull` tag specifically so that a zero value (a bus stating no
// voltage, the ordinary case for every IEEE case in the corpus) can be
// stored. A stray notnull tag compiles, round-trips against the in-memory
// store, and is refused only here.
func TestBusesRoundTripThroughPostgres(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	mdl, err := s.Models().Create(ctx, aModel(hashFor("bus-round-trip")))
	if err != nil {
		t.Fatalf("Models().Create: %v", err)
	}

	want := []model.Bus{
		aBus(mdl.ID, "bus-1", 345000),
		aBus(mdl.ID, "bus-2", 0), // states no voltage
		aBus(mdl.ID, "bus-3", 138000),
	}

	if err := s.Buses().PutAll(ctx, mdl.ID, want); err != nil {
		t.Fatalf("Buses().PutAll: %v", err)
	}

	got, err := s.Buses().ForModel(ctx, mdl.ID)
	if err != nil {
		t.Fatalf("Buses().ForModel: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("ForModel returned %d buses, want %d", len(got), len(want))
	}

	// ForModel orders by ordinal number, which happens to match want's order
	// here (bus-1, bus-2, bus-3).
	for i, w := range want {
		if got[i].Ordinal != w.Ordinal || got[i].VNominalV != w.VNominalV {
			t.Errorf("bus %d = %+v, want ordinal %s at %v volts",
				i, got[i], w.Ordinal, w.VNominalV)
		}
	}

	// The zero-voltage bus specifically, read back by the lookup a branch
	// actually makes — not as an absence, but as a bus that states zero.
	silent, err := s.Buses().ByOrdinal(ctx, mdl.ID, "bus-2")
	if err != nil {
		t.Fatalf("ByOrdinal(bus-2): %v", err)
	}

	if silent.VNominalV != 0 {
		t.Errorf("bus-2 VNominalV = %v, want 0", silent.VNominalV)
	}

	stated, err := s.Buses().ByOrdinal(ctx, mdl.ID, "bus-1")
	if err != nil {
		t.Fatalf("ByOrdinal(bus-1): %v", err)
	}

	if stated.VNominalV != 345000 {
		t.Errorf("bus-1 VNominalV = %v, want 345000", stated.VNominalV)
	}
}

// PutAll replaces a model's buses rather than merging with what is already
// there: a second call with a disjoint ordinal set leaves none of the first
// set behind.
func TestBusPutAllReplaces(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	mdl, err := s.Models().Create(ctx, aModel(hashFor("bus-put-all-replaces")))
	if err != nil {
		t.Fatalf("Models().Create: %v", err)
	}

	first := []model.Bus{aBus(mdl.ID, "bus-1", 1000), aBus(mdl.ID, "bus-2", 2000)}
	if err := s.Buses().PutAll(ctx, mdl.ID, first); err != nil {
		t.Fatalf("Buses().PutAll (first): %v", err)
	}

	second := []model.Bus{aBus(mdl.ID, "bus-9", 9000)}
	if err := s.Buses().PutAll(ctx, mdl.ID, second); err != nil {
		t.Fatalf("Buses().PutAll (second): %v", err)
	}

	got, err := s.Buses().ForModel(ctx, mdl.ID)
	if err != nil {
		t.Fatalf("Buses().ForModel: %v", err)
	}

	if len(got) != 1 || got[0].Ordinal != "bus-9" {
		t.Fatalf("ForModel after replace = %+v, want exactly [bus-9]", got)
	}

	if _, err := s.Buses().ByOrdinal(ctx, mdl.ID, "bus-1"); !errors.Is(err, bserr.ErrNotFound) {
		t.Errorf("ByOrdinal(bus-1) after replace: err = %v, want bserr.ErrNotFound", err)
	}
}

// A bus ordinal ByOrdinal has never heard of is bserr.ErrNotFound, not a bus
// that happens to read back zeroed.
func TestBusByOrdinalMissingIsNotFound(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	mdl, err := s.Models().Create(ctx, aModel(hashFor("bus-missing-ordinal")))
	if err != nil {
		t.Fatalf("Models().Create: %v", err)
	}

	_, err = s.Buses().ByOrdinal(ctx, mdl.ID, "bus-does-not-exist")
	if !errors.Is(err, bserr.ErrNotFound) {
		t.Fatalf("ByOrdinal error = %v, want bserr.ErrNotFound", err)
	}
}

// Migration 0004's unique index on (tenant_id, model_id, ordinal) is what
// stops two rows from claiming the same bus — a contradiction, not a
// duplicate, per the migration's own comment. Only a real database enforces
// it; nothing in Go stops PutAll from handing it two rows for "bus-1".
func TestBusOrdinalUniqueIndexRejectsDuplicate(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	mdl, err := s.Models().Create(ctx, aModel(hashFor("bus-duplicate-ordinal")))
	if err != nil {
		t.Fatalf("Models().Create: %v", err)
	}

	dupes := []model.Bus{
		aBus(mdl.ID, "bus-1", 1000),
		aBus(mdl.ID, "bus-1", 2000), // same model, same ordinal, different row
	}

	err = s.Buses().PutAll(ctx, mdl.ID, dupes)
	if !errors.Is(err, bserr.ErrConflict) {
		t.Fatalf("PutAll with a duplicate ordinal: err = %v, want bserr.ErrConflict", err)
	}
}

// AddMissing is repairIdentity's write path against a real database: an
// ordinal that already has a row must keep that row's identifier, even when
// the caller (as repairIdentity always does) hands it a freshly-minted row
// for that same ordinal alongside the ones that are actually missing.
//
// This is the regression PutAll's read-then-delete-then-create shape cannot
// pass: deleting bus-1 before rewriting it, even to write back a bus-1,
// hands the row a new identifier the moment it is re-created. Only a store
// that never deletes an ordinal it already has can keep the old one.
func TestBusAddMissingKeepsExistingIdentifiers(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	mdl, err := s.Models().Create(ctx, aModel(hashFor("bus-add-missing-identity")))
	if err != nil {
		t.Fatalf("Models().Create: %v", err)
	}

	existing := aBus(mdl.ID, "bus-1", 345000)
	if err := s.Buses().PutAll(ctx, mdl.ID, []model.Bus{existing}); err != nil {
		t.Fatalf("Buses().PutAll: %v", err)
	}

	// What repairIdentity actually builds: the full parsed set, including a
	// freshly-minted row for the ordinal that is already stored.
	freshOne := aBus(mdl.ID, "bus-1", 345000)
	two := aBus(mdl.ID, "bus-2", 138000)

	n, err := s.Buses().AddMissing(ctx, mdl.ID, []model.Bus{freshOne, two})
	if err != nil {
		t.Fatalf("Buses().AddMissing: %v", err)
	}

	if n != 1 {
		t.Fatalf("AddMissing inserted %d, want 1 (only bus-2)", n)
	}

	got, err := s.Buses().ByOrdinal(ctx, mdl.ID, "bus-1")
	if err != nil {
		t.Fatalf("ByOrdinal(bus-1): %v", err)
	}

	if got.ID != existing.ID {
		t.Errorf("bus-1's identifier changed: got %s, want the original %s", got.ID, existing.ID)
	}

	if _, err := s.Buses().ByOrdinal(ctx, mdl.ID, "bus-2"); err != nil {
		t.Errorf("ByOrdinal(bus-2) after AddMissing: %v", err)
	}

	all, err := s.Buses().ForModel(ctx, mdl.ID)
	if err != nil {
		t.Fatalf("Buses().ForModel: %v", err)
	}

	if len(all) != 2 {
		t.Fatalf("ForModel after AddMissing returned %d buses, want 2", len(all))
	}
}

// The fully-empty case against a real database: AddMissing on a model with no
// rows behaves like a fresh write, inserting everything it is handed.
func TestBusAddMissingOnAnEmptyModelInsertsEverything(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	mdl, err := s.Models().Create(ctx, aModel(hashFor("bus-add-missing-empty")))
	if err != nil {
		t.Fatalf("Models().Create: %v", err)
	}

	in := []model.Bus{aBus(mdl.ID, "bus-1", 1000), aBus(mdl.ID, "bus-2", 2000)}

	n, err := s.Buses().AddMissing(ctx, mdl.ID, in)
	if err != nil {
		t.Fatalf("Buses().AddMissing: %v", err)
	}

	if n != len(in) {
		t.Fatalf("AddMissing inserted %d, want %d", n, len(in))
	}
}

// The fully-complete case against a real database: every ordinal handed to
// AddMissing is already stored, so nothing is inserted, and — this is the
// part a count-only assertion would miss — the unique index on
// (tenant_id, model_id, ordinal) is never even challenged, because AddMissing
// filters client-side rather than attempting the insert and tolerating the
// conflict.
func TestBusAddMissingOnACompleteModelInsertsNothing(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	mdl, err := s.Models().Create(ctx, aModel(hashFor("bus-add-missing-complete")))
	if err != nil {
		t.Fatalf("Models().Create: %v", err)
	}

	in := []model.Bus{aBus(mdl.ID, "bus-1", 1000), aBus(mdl.ID, "bus-2", 2000)}
	if err := s.Buses().PutAll(ctx, mdl.ID, in); err != nil {
		t.Fatalf("Buses().PutAll: %v", err)
	}

	fresh := []model.Bus{aBus(mdl.ID, "bus-1", 1000), aBus(mdl.ID, "bus-2", 2000)}

	n, err := s.Buses().AddMissing(ctx, mdl.ID, fresh)
	if err != nil {
		t.Fatalf("Buses().AddMissing on a complete model: %v", err)
	}

	if n != 0 {
		t.Errorf("AddMissing inserted %d on a complete model, want 0", n)
	}
}

// branches.Get is the single lookup assumeCandidates makes to reach a
// branch's model from a bare br_ identifier, so it is worth its own
// round-trip against a real database rather than trusting the memory store's
// map lookup to generalize.
func TestBranchGetFindsByIdentifier(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	mdl, err := s.Models().Create(ctx, aModel(hashFor("branch-get")))
	if err != nil {
		t.Fatalf("Models().Create: %v", err)
	}

	want := aBranch(mdl.ID, "branch-1", "bus-1", "bus-2")

	if err := s.Branches().PutAll(ctx, mdl.ID, []model.Branch{want}); err != nil {
		t.Fatalf("Branches().PutAll: %v", err)
	}

	got, err := s.Branches().Get(ctx, want.ID)
	if err != nil {
		t.Fatalf("Branches().Get: %v", err)
	}

	if got.ID != want.ID || got.ModelID != want.ModelID || got.Ordinal != want.Ordinal ||
		got.FromBus != want.FromBus || got.ToBus != want.ToBus {
		t.Errorf("Get returned %+v, want %+v", got, want)
	}
}

// A branch identifier nothing ever wrote is bserr.ErrNotFound, which is what
// lets assumeCandidates report a stale route rather than fail the whole pass.
func TestBranchGetMissingIsNotFound(t *testing.T) {
	s := harness(t)

	_, err := s.Branches().Get(context.Background(), nmid.NewBranch())
	if !errors.Is(err, bserr.ErrNotFound) {
		t.Fatalf("Get error = %v, want bserr.ErrNotFound", err)
	}
}

// The branch-side twin of TestBusAddMissingKeepsExistingIdentifiers, and the
// test that matters most in this file: it reproduces exactly the state a
// route's CarrierID and a hazard conductor assignment depend on staying
// resolvable through a repair — a branch that already has a row keeps its
// identifier even when handed a freshly-minted row for its own ordinal
// alongside the ordinal that is actually missing.
func TestBranchAddMissingKeepsExistingIdentifiers(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	mdl, err := s.Models().Create(ctx, aModel(hashFor("branch-add-missing-identity")))
	if err != nil {
		t.Fatalf("Models().Create: %v", err)
	}

	existing := aBranch(mdl.ID, "branch-0", "bus-1", "bus-2")
	if err := s.Branches().PutAll(ctx, mdl.ID, []model.Branch{existing}); err != nil {
		t.Fatalf("Branches().PutAll: %v", err)
	}

	freshZero := aBranch(mdl.ID, "branch-0", "bus-1", "bus-2")
	one := aBranch(mdl.ID, "branch-1", "bus-2", "bus-3")

	n, err := s.Branches().AddMissing(ctx, mdl.ID, []model.Branch{freshZero, one})
	if err != nil {
		t.Fatalf("Branches().AddMissing: %v", err)
	}

	if n != 1 {
		t.Fatalf("AddMissing inserted %d, want 1 (only branch-1)", n)
	}

	got, err := s.Branches().Get(ctx, existing.ID)
	if err != nil {
		t.Fatalf("Branches().Get(%s) after AddMissing: %v", existing.ID, err)
	}

	if got.Ordinal != "branch-0" {
		t.Errorf("branch-0's own row came back naming ordinal %s", got.Ordinal)
	}

	byOrdinal, err := s.Branches().ByOrdinal(ctx, mdl.ID, "branch-0")
	if err != nil {
		t.Fatalf("ByOrdinal(branch-0): %v", err)
	}

	if byOrdinal.ID != existing.ID {
		t.Errorf("branch-0's identifier changed: got %s, want the original %s", byOrdinal.ID, existing.ID)
	}

	if _, err := s.Branches().ByOrdinal(ctx, mdl.ID, "branch-1"); err != nil {
		t.Errorf("ByOrdinal(branch-1) after AddMissing: %v", err)
	}
}

// The fully-empty case against a real database: AddMissing on a model with no
// branch rows inserts everything it is handed.
func TestBranchAddMissingOnAnEmptyModelInsertsEverything(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	mdl, err := s.Models().Create(ctx, aModel(hashFor("branch-add-missing-empty")))
	if err != nil {
		t.Fatalf("Models().Create: %v", err)
	}

	in := []model.Branch{
		aBranch(mdl.ID, "branch-0", "bus-1", "bus-2"),
		aBranch(mdl.ID, "branch-1", "bus-2", "bus-3"),
	}

	n, err := s.Branches().AddMissing(ctx, mdl.ID, in)
	if err != nil {
		t.Fatalf("Branches().AddMissing: %v", err)
	}

	if n != len(in) {
		t.Fatalf("AddMissing inserted %d, want %d", n, len(in))
	}
}

// The fully-complete case against a real database: every ordinal already has
// a row, so AddMissing inserts nothing and never attempts an insert the
// unique index on (tenant_id, model_id, ordinal) would reject.
func TestBranchAddMissingOnACompleteModelInsertsNothing(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	mdl, err := s.Models().Create(ctx, aModel(hashFor("branch-add-missing-complete")))
	if err != nil {
		t.Fatalf("Models().Create: %v", err)
	}

	in := []model.Branch{
		aBranch(mdl.ID, "branch-0", "bus-1", "bus-2"),
		aBranch(mdl.ID, "branch-1", "bus-2", "bus-3"),
	}
	if err := s.Branches().PutAll(ctx, mdl.ID, in); err != nil {
		t.Fatalf("Branches().PutAll: %v", err)
	}

	fresh := []model.Branch{
		aBranch(mdl.ID, "branch-0", "bus-1", "bus-2"),
		aBranch(mdl.ID, "branch-1", "bus-2", "bus-3"),
	}

	n, err := s.Branches().AddMissing(ctx, mdl.ID, fresh)
	if err != nil {
		t.Fatalf("Branches().AddMissing on a complete model: %v", err)
	}

	if n != 0 {
		t.Errorf("AddMissing inserted %d on a complete model, want 0", n)
	}
}

// Tenant isolation actually isolates: a bus and a branch written under one
// tenant are unreachable — by ByOrdinal, by Get, and by the model row they
// belong to — from a store scoped to a different tenant on the same Fabriq
// facade and the same underlying tables.
//
// This is the test that would fail if migration 0004 forgot FORCE ROW LEVEL
// SECURITY on blackstart_networkmodel_bus, or if a policy were missing or
// wrong: the harness connects as a non-superuser specifically so that row
// security is not silently bypassed by the connection's own privilege.
func TestTenantIsolation(t *testing.T) {
	fab := openFabriq(t)
	ctx := context.Background()

	storeA := storeFor(t, fab, "blackstart-tenant-a")
	storeB := storeFor(t, fab, "blackstart-tenant-b")

	mdl, err := storeA.Models().Create(ctx, aModel(hashFor("tenant-isolation")))
	if err != nil {
		t.Fatalf("tenant A Models().Create: %v", err)
	}

	bus := aBus(mdl.ID, "bus-1", 138000)
	if err := storeA.Buses().PutAll(ctx, mdl.ID, []model.Bus{bus}); err != nil {
		t.Fatalf("tenant A Buses().PutAll: %v", err)
	}

	branch := aBranch(mdl.ID, "branch-1", "bus-1", "bus-2")
	if err := storeA.Branches().PutAll(ctx, mdl.ID, []model.Branch{branch}); err != nil {
		t.Fatalf("tenant A Branches().PutAll: %v", err)
	}

	if _, err := storeB.Models().Get(ctx, mdl.ID); !errors.Is(err, bserr.ErrNotFound) {
		t.Errorf("tenant B Models().Get: err = %v, want bserr.ErrNotFound", err)
	}

	if _, err := storeB.Buses().ByOrdinal(ctx, mdl.ID, "bus-1"); !errors.Is(err, bserr.ErrNotFound) {
		t.Errorf("tenant B Buses().ByOrdinal: err = %v, want bserr.ErrNotFound", err)
	}

	if _, err := storeB.Branches().Get(ctx, branch.ID); !errors.Is(err, bserr.ErrNotFound) {
		t.Errorf("tenant B Branches().Get: err = %v, want bserr.ErrNotFound", err)
	}

	// Tenant A still sees its own rows — isolation must not have become
	// mutual erasure.
	if _, err := storeA.Buses().ByOrdinal(ctx, mdl.ID, "bus-1"); err != nil {
		t.Errorf("tenant A Buses().ByOrdinal after isolation check: %v", err)
	}
}
