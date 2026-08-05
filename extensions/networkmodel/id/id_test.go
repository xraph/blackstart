package id_test

import (
	"strings"
	"testing"

	nmid "github.com/xraph/blackstart/extensions/networkmodel/id"
)

// A branch identifier is minted under its own prefix.
//
// The prefix is what makes a branch addressable at all: a cascade event names
// "branch-3", and the only way that becomes a row is if branches have
// identity of their own.
func TestNewBranchCarriesTheBranchPrefix(t *testing.T) {
	t.Parallel()

	got := nmid.NewBranch()

	if !strings.HasPrefix(got.String(), "br_") {
		t.Errorf("NewBranch() = %q, want a br_ prefix", got)
	}
}

func TestNewPlacementCarriesThePlacementPrefix(t *testing.T) {
	t.Parallel()

	got := nmid.NewPlacement()

	if !strings.HasPrefix(got.String(), "nmplc_") {
		t.Errorf("NewPlacement() = %q, want an nmplc_ prefix", got)
	}
}
