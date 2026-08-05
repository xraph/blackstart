package scenario

// These tests live inside the package because the pipeline's stages are
// unexported: the engine owns them, and nothing outside the extension calls
// one on its own.

import (
	"testing"
	"time"

	scnid "github.com/xraph/blackstart/extensions/scenario/id"
	scn "github.com/xraph/blackstart/extensions/scenario/scenario"
	scnstore "github.com/xraph/blackstart/extensions/scenario/store"
	"github.com/xraph/blackstart/extensions/scenario/store/memory"
)

// validManifest is the manifest the freeze test names by value: its model
// hash is asserted literally, because the point of that test is that the
// stored run kept THIS hash after the scenario moved to another.
func validManifest() scn.Manifest {
	return scn.Manifest{
		ModelHash:    "sha256:abc",
		WeatherCycle: theCycle.ID.String(),
		ValidAt:      valid,
		Policy:       scn.PolicyAll,
		Seed:         42,
	}
}

// engineWith builds an engine over a fresh memory store and a pinned clock.
func engineWith(t *testing.T, f *fakes) (*Engine, scnstore.Store) {
	t.Helper()

	store := memory.New()

	engine, err := NewEngine(store, f.collaborators(), nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	// A fixed clock, so a recorded time is a value a test can assert rather
	// than a moving target it has to bound.
	engine.now = func() time.Time { return issued }

	return engine, store
}

// engineWithFakes is engineWith over a collaborator set nothing has broken.
func engineWithFakes(t *testing.T) (*Engine, scnstore.Store) {
	t.Helper()

	return engineWith(t, newFakes())
}

// A run freezes its manifest. Editing the scenario afterwards must not reach
// a run that already exists.
//
// This is THE reproducibility invariant. If the engine re-read the scenario
// during Execute — or stored a pointer rather than a copy — then a stored run
// would describe inputs it was not computed from, and "same manifest, same
// result" would be a claim nobody could check.
func TestEnqueueFreezesTheManifest(t *testing.T) {
	t.Parallel()

	e, store := engineWithFakes(t)

	defined, err := e.Define(t.Context(), scn.Scenario{
		Name: "derecho", Manifest: validManifest(),
	})
	if err != nil {
		t.Fatalf("define: %v", err)
	}

	queued, err := e.Enqueue(t.Context(), defined.ID)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Move the scenario to a different model entirely.
	edited := defined
	edited.Manifest.ModelHash = "sha256:something-else"
	if _, err := e.Define(t.Context(), edited); err != nil {
		t.Fatalf("redefine: %v", err)
	}

	stored, err := store.Runs().Get(t.Context(), queued.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}

	if stored.Manifest.ModelHash != "sha256:abc" {
		t.Errorf("the run's manifest followed the scenario: got %q, want the frozen %q",
			stored.Manifest.ModelHash, "sha256:abc")
	}
}

// The test above can only fail if the run and the scenario share memory, and
// with a Manifest value they share exactly one thing: the array behind
// Branches. This is that case, and it is the one a plain struct copy gets
// wrong while looking correct.
//
// The edit goes through the store rather than through the caller's own slice,
// because that is what "editing the scenario" means — and because it is the
// path a copy in Define would not protect.
func TestEnqueueFreezesTheBranchListToo(t *testing.T) {
	t.Parallel()

	e, store := engineWithFakes(t)

	manifest := validManifest()
	manifest.Policy = scn.PolicyExplicit
	manifest.Branches = []string{"nmdl_one", "nmdl_two"}

	defined, err := e.Define(t.Context(), scn.Scenario{Name: "derecho", Manifest: manifest})
	if err != nil {
		t.Fatalf("define: %v", err)
	}

	queued, err := e.Enqueue(t.Context(), defined.ID)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Edit the scenario's own branch list in place, which is what a slice
	// makes possible and a scalar does not.
	held, err := store.Scenarios().Get(t.Context(), defined.ID)
	if err != nil {
		t.Fatalf("get scenario: %v", err)
	}

	held.Manifest.Branches[0] = "nmdl_edited"

	stored, err := store.Runs().Get(t.Context(), queued.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}

	if stored.Manifest.Branches[0] != "nmdl_one" {
		t.Errorf("the run's branch list followed the scenario: got %q, want the frozen %q",
			stored.Manifest.Branches[0], "nmdl_one")
	}
}

// A queued run is queued: no failure, no children, and no rating counts —
// nil rather than zero, because a run that has not looked is not a run that
// rated nothing.
func TestEnqueueStoresAQueuedRun(t *testing.T) {
	t.Parallel()

	e, store := engineWithFakes(t)

	defined, err := e.Define(t.Context(), scn.Scenario{
		Name: "derecho", Manifest: validManifest(),
	})
	if err != nil {
		t.Fatalf("define: %v", err)
	}

	queued, err := e.Enqueue(t.Context(), defined.ID)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	stored, err := store.Runs().Get(t.Context(), queued.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}

	if stored.Status != scn.StatusQueued {
		t.Errorf("status = %q, want queued", stored.Status)
	}

	if stored.ScenarioID != defined.ID {
		t.Errorf("run names scenario %s, want %s", stored.ScenarioID, defined.ID)
	}

	if stored.QueuedAt.IsZero() {
		t.Error("a queued run has no queue time")
	}

	if stored.WeatherRated != nil || stored.BranchCount != nil {
		t.Errorf("a queued run reports coverage it cannot have: %+v", stored)
	}
}

// Enqueueing a scenario nobody defined is not found rather than a run against
// a zero manifest.
func TestEnqueueOfAnUnknownScenarioIsNotFound(t *testing.T) {
	t.Parallel()

	e, _ := engineWithFakes(t)

	if _, err := e.Enqueue(t.Context(), scnid.NewScenario()); err == nil {
		t.Fatal("want an error: no such scenario")
	}
}

// Defining a scenario mints an identifier and stamps both times; redefining
// it moves UpdatedAt and leaves CreatedAt where it was, because a renamed
// scenario is the same scenario.
func TestDefineMintsAnIdentifierAndKeepsTheCreationTime(t *testing.T) {
	t.Parallel()

	f := newFakes()
	e, _ := engineWith(t, f)

	defined, err := e.Define(t.Context(), scn.Scenario{
		Name: "derecho", Manifest: validManifest(),
	})
	if err != nil {
		t.Fatalf("define: %v", err)
	}

	if defined.ID.IsZero() {
		t.Fatal("Define minted no identifier")
	}

	if !defined.CreatedAt.Equal(issued) {
		t.Errorf("created at %v, want the engine's clock %v", defined.CreatedAt, issued)
	}

	later := issued.Add(time.Hour)
	e.now = func() time.Time { return later }

	edited := defined
	edited.Name = "renamed"

	updated, err := e.Define(t.Context(), edited)
	if err != nil {
		t.Fatalf("redefine: %v", err)
	}

	if !updated.CreatedAt.Equal(issued) {
		t.Errorf("an edit moved the creation time to %v", updated.CreatedAt)
	}

	if !updated.UpdatedAt.Equal(later) {
		t.Errorf("an edit left UpdatedAt at %v", updated.UpdatedAt)
	}
}

// A manifest that cannot be reproduced from is refused before anything is
// stored.
func TestDefineRefusesAnUnreproducibleManifest(t *testing.T) {
	t.Parallel()

	e, store := engineWithFakes(t)

	broken := validManifest()
	broken.WeatherCycle = ""

	if _, err := e.Define(t.Context(), scn.Scenario{Name: "nameless", Manifest: broken}); err == nil {
		t.Fatal("want an error: a manifest with no weather cycle")
	}

	listed, err := store.Scenarios().List(t.Context(), 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(listed) != 0 {
		t.Errorf("%d scenarios stored despite the refusal", len(listed))
	}
}
