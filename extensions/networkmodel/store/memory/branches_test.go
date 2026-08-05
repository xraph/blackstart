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
