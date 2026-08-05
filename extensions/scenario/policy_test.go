package scenario

import (
	"strings"
	"testing"

	scn "github.com/xraph/blackstart/extensions/scenario/scenario"
	solverv1 "github.com/xraph/blackstart/proto/gen/go/blackstart/solver/v1"
)

// PolicyAll passes no contingencies, which the contingency contract reads as
// every in-service branch. An empty list is not the same as a list of none.
func TestPolicyAllNamesNoContingencies(t *testing.T) {
	t.Parallel()

	got, err := contingenciesFor(validManifest(), fiveBranchModel(345_000))
	if err != nil {
		t.Fatalf("contingenciesFor: %v", err)
	}

	if got != nil {
		t.Errorf("policy all produced %d contingencies, want none", len(got))
	}
}

// PolicyExplicit passes its branches through as the ordering, in the
// manifest's own order: the ordering is the caller's deliberately, because
// this is the seam a risk model plugs into.
func TestPolicyExplicitPassesItsOrderingThrough(t *testing.T) {
	t.Parallel()

	manifest := validManifest()
	manifest.Policy = scn.PolicyExplicit
	manifest.Branches = []string{lines[3].String(), lines[1].String()}

	got, err := contingenciesFor(manifest, fiveBranchModel(345_000))
	if err != nil {
		t.Fatalf("contingenciesFor: %v", err)
	}

	want := []*solverv1.Contingency{
		{Id: lines[3].String(), Branch: 3},
		{Id: lines[1].String(), Branch: 1},
	}

	if len(got) != len(want) {
		t.Fatalf("got %d contingencies, want %d", len(got), len(want))
	}

	for index, one := range want {
		if got[index].GetId() != one.GetId() || got[index].GetBranch() != one.GetBranch() {
			t.Errorf("position %d: got %s at index %d, want %s at %d", index,
				got[index].GetId(), got[index].GetBranch(), one.GetId(), one.GetBranch())
		}
	}
}

// A policy naming a branch the pinned model does not contain fails naming it.
// Screening the rest would answer a question nobody asked.
func TestAPolicyNamingAnAbsentBranchFails(t *testing.T) {
	t.Parallel()

	manifest := validManifest()
	manifest.Policy = scn.PolicyExplicit
	manifest.Branches = []string{lines[0].String(), "nmdl_not_in_this_model"}

	_, err := contingenciesFor(manifest, fiveBranchModel(345_000))
	if err == nil {
		t.Fatal("want an error: the policy names a branch the model does not have")
	}

	if !strings.Contains(err.Error(), "nmdl_not_in_this_model") {
		t.Errorf("the error does not name the missing branch: %v", err)
	}
}
