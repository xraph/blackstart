package memory_test

import (
	"errors"
	"testing"

	nmid "github.com/xraph/blackstart/extensions/networkmodel/id"
	"github.com/xraph/blackstart/extensions/networkmodel/model"
	"github.com/xraph/blackstart/extensions/networkmodel/store/memory"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// The lookup a route makes. A route names its carrier and nothing else, so
// without this there is no path from a br_ identifier to the model whose
// buses state the voltage it runs at.
func TestGetFindsABranchByItsIdentifier(t *testing.T) {
	t.Parallel()

	s := memory.New()
	modelID := nmid.NewModel()

	want := model.Branch{
		ID:      nmid.NewBranch(),
		ModelID: modelID,
		Ordinal: "branch-0",
		FromBus: "bus-1",
		ToBus:   "bus-2",
	}

	if err := s.Branches().PutAll(t.Context(), modelID, []model.Branch{want}); err != nil {
		t.Fatalf("writing branches: %v", err)
	}

	got, err := s.Branches().Get(t.Context(), want.ID)
	if err != nil {
		t.Fatalf("reading %s: %v", want.ID, err)
	}

	if got.ModelID != modelID {
		t.Fatalf("branch came back under model %s, want %s", got.ModelID, modelID)
	}

	if got.FromBus != "bus-1" {
		t.Fatalf("branch came back from %s, want bus-1", got.FromBus)
	}
}

func TestGetReportsAMissingBranch(t *testing.T) {
	t.Parallel()

	s := memory.New()

	_, err := s.Branches().Get(t.Context(), nmid.NewBranch())
	if !errors.Is(err, bserr.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func branches(modelID id.ID, ordinals ...string) []model.Branch {
	out := make([]model.Branch, 0, len(ordinals))

	for i, ordinal := range ordinals {
		out = append(out, model.Branch{
			ID:      nmid.NewBranch(),
			ModelID: modelID,
			Ordinal: ordinal,
			FromBus: "bus-" + ordinal,
			ToBus:   "bus-end-" + ordinal,
			Alias:   string(rune('a' + i)),
		})
	}

	return out
}

func TestByOrdinalFindsTheBranchTheSolverNames(t *testing.T) {
	t.Parallel()

	s := memory.New()
	modelID := nmid.NewModel()
	in := branches(modelID, "branch-0", "branch-1", "branch-2")

	if err := s.Branches().PutAll(t.Context(), modelID, in); err != nil {
		t.Fatalf("PutAll: %v", err)
	}

	got, err := s.Branches().ByOrdinal(t.Context(), modelID, "branch-1")
	if err != nil {
		t.Fatalf("ByOrdinal: %v", err)
	}

	if got.ID != in[1].ID {
		t.Errorf("ByOrdinal returned %s, want %s", got.ID, in[1].ID)
	}
}

// An ordinal no model has means the run and the model disagree, which is
// worth reporting rather than returning an empty branch that reads as one
// connecting nothing to nothing.
func TestByOrdinalReportsAnOrdinalTheModelDoesNotHave(t *testing.T) {
	t.Parallel()

	s := memory.New()
	modelID := nmid.NewModel()

	if err := s.Branches().PutAll(t.Context(), modelID, branches(modelID, "branch-0")); err != nil {
		t.Fatalf("PutAll: %v", err)
	}

	_, err := s.Branches().ByOrdinal(t.Context(), modelID, "branch-99")
	if !errors.Is(err, bserr.ErrNotFound) {
		t.Errorf("ByOrdinal = %v, want ErrNotFound", err)
	}
}

// Ordinals are scoped to their model. Two cases both have a branch-0, and a
// lookup that crossed models would return another network's conductor.
func TestByOrdinalDoesNotCrossModels(t *testing.T) {
	t.Parallel()

	s := memory.New()
	mine, theirs := nmid.NewModel(), nmid.NewModel()

	if err := s.Branches().PutAll(t.Context(), theirs, branches(theirs, "branch-0")); err != nil {
		t.Fatalf("PutAll: %v", err)
	}

	if _, err := s.Branches().ByOrdinal(t.Context(), mine, "branch-0"); !errors.Is(err, bserr.ErrNotFound) {
		t.Errorf("ByOrdinal crossed models: %v", err)
	}
}

// Re-importing the same file writes the same ordinals again. The second write
// must leave one branch per ordinal, or every lookup afterwards is ambiguous.
func TestPutAllReplacesRatherThanAccumulates(t *testing.T) {
	t.Parallel()

	s := memory.New()
	modelID := nmid.NewModel()

	for range 2 {
		if err := s.Branches().PutAll(
			t.Context(), modelID, branches(modelID, "branch-0", "branch-1")); err != nil {
			t.Fatalf("PutAll: %v", err)
		}
	}

	got, err := s.Branches().ForModel(t.Context(), modelID)
	if err != nil {
		t.Fatalf("ForModel: %v", err)
	}

	if len(got) != 2 {
		t.Errorf("ForModel returned %d branches after two writes, want 2", len(got))
	}
}

// AddMissing on a model with no rows at all behaves like a fresh write: every
// ordinal handed to it lands, and the count returned says so.
func TestAddMissingOnAnEmptyModelInsertsEverything(t *testing.T) {
	t.Parallel()

	s := memory.New()
	modelID := nmid.NewModel()
	in := branches(modelID, "branch-0", "branch-1", "branch-2")

	n, err := s.Branches().AddMissing(t.Context(), modelID, in)
	if err != nil {
		t.Fatalf("AddMissing: %v", err)
	}

	if n != len(in) {
		t.Errorf("AddMissing inserted %d, want %d", n, len(in))
	}

	got, err := s.Branches().ForModel(t.Context(), modelID)
	if err != nil {
		t.Fatalf("ForModel: %v", err)
	}

	if len(got) != len(in) {
		t.Fatalf("ForModel returned %d branches, want %d", len(got), len(in))
	}
}

// The case AddMissing exists for: a model with branch-0 already stored and
// branch-1 missing. AddMissing must insert only branch-1, and branch-0's row
// — identifier included — must come back exactly as it went in, not
// replaced by whatever freshly-minted branch-0 the caller also handed it.
func TestAddMissingInsertsOnlyTheMissingOrdinalAndKeepsTheRest(t *testing.T) {
	t.Parallel()

	s := memory.New()
	modelID := nmid.NewModel()

	existing := branches(modelID, "branch-0")[0]

	if err := s.Branches().PutAll(t.Context(), modelID, []model.Branch{existing}); err != nil {
		t.Fatalf("PutAll: %v", err)
	}

	// A repair hands AddMissing a freshly-minted branch-0 alongside the
	// actually-missing branch-1 — repairIdentity cannot know in advance which
	// ordinals are already stored, so it always builds the full set. A
	// freshly-minted branch-0 must be discarded, not written.
	freshZero := branches(modelID, "branch-0")[0]
	freshZero.ID = nmid.NewBranch()
	one := branches(modelID, "branch-1")[0]

	n, err := s.Branches().AddMissing(t.Context(), modelID, []model.Branch{freshZero, one})
	if err != nil {
		t.Fatalf("AddMissing: %v", err)
	}

	if n != 1 {
		t.Fatalf("AddMissing inserted %d, want 1 (only branch-1)", n)
	}

	got, err := s.Branches().ByOrdinal(t.Context(), modelID, "branch-0")
	if err != nil {
		t.Fatalf("ByOrdinal(branch-0): %v", err)
	}

	if got.ID != existing.ID {
		t.Errorf("branch-0's identifier changed: got %s, want the original %s", got.ID, existing.ID)
	}

	if _, err := s.Branches().ByOrdinal(t.Context(), modelID, "branch-1"); err != nil {
		t.Errorf("ByOrdinal(branch-1) after AddMissing: %v", err)
	}
}

// The fully-complete case: every ordinal handed to AddMissing already
// exists, so nothing is inserted and nothing already stored changes.
func TestAddMissingOnACompleteModelInsertsNothing(t *testing.T) {
	t.Parallel()

	s := memory.New()
	modelID := nmid.NewModel()

	in := branches(modelID, "branch-0", "branch-1")
	if err := s.Branches().PutAll(t.Context(), modelID, in); err != nil {
		t.Fatalf("PutAll: %v", err)
	}

	// Freshly-minted rows for ordinals the model already has, exactly what
	// repairIdentity would build from the parsed case.
	fresh := branches(modelID, "branch-0", "branch-1")

	n, err := s.Branches().AddMissing(t.Context(), modelID, fresh)
	if err != nil {
		t.Fatalf("AddMissing: %v", err)
	}

	if n != 0 {
		t.Errorf("AddMissing inserted %d on a complete model, want 0", n)
	}

	got, err := s.Branches().ForModel(t.Context(), modelID)
	if err != nil {
		t.Fatalf("ForModel: %v", err)
	}

	for i, branch := range got {
		if branch.ID != in[i].ID {
			t.Errorf("branch %s's identifier changed: got %s, want the original %s",
				branch.Ordinal, branch.ID, in[i].ID)
		}
	}
}

// AddMissing validates and checks model ownership exactly as PutAll does —
// it is still a write path, and a caller that got the model wrong deserves
// the same rejection either way.
func TestAddMissingRejectsABranchFromAnotherModel(t *testing.T) {
	t.Parallel()

	s := memory.New()
	modelID := nmid.NewModel()

	_, err := s.Branches().AddMissing(t.Context(), modelID, branches(nmid.NewModel(), "branch-0"))
	if !errors.Is(err, bserr.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
}

// Ordinal order, not map order. A caller walking a model's branches to trace
// them expects the network in the order the case declares it.
func TestForModelReturnsBranchesInOrdinalOrder(t *testing.T) {
	t.Parallel()

	s := memory.New()
	modelID := nmid.NewModel()

	if err := s.Branches().PutAll(
		t.Context(), modelID, branches(modelID, "branch-2", "branch-0", "branch-10")); err != nil {
		t.Fatalf("PutAll: %v", err)
	}

	got, err := s.Branches().ForModel(t.Context(), modelID)
	if err != nil {
		t.Fatalf("ForModel: %v", err)
	}

	want := []string{"branch-0", "branch-2", "branch-10"}
	for i, branch := range got {
		if branch.Ordinal != want[i] {
			t.Errorf("branch %d is %s, want %s", i, branch.Ordinal, want[i])
		}
	}
}
