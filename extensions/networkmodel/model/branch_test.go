package model_test

import (
	"errors"
	"testing"

	nmid "github.com/xraph/blackstart/extensions/networkmodel/id"
	"github.com/xraph/blackstart/extensions/networkmodel/model"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

func aBranch() model.Branch {
	return model.Branch{
		ID:      nmid.NewBranch(),
		ModelID: nmid.NewModel(),
		Ordinal: "branch-3",
		FromBus: "bus-1",
		ToBus:   "bus-2",
		Alias:   "4-7",
	}
}

func TestAValidBranchValidates(t *testing.T) {
	t.Parallel()

	if err := aBranch().Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

// Each field is load-bearing for a different reason, so each is checked
// separately rather than through one "is it complete" assertion.
func TestABranchMissingAnythingIsInvalid(t *testing.T) {
	t.Parallel()

	for name, break_ := range map[string]func(*model.Branch){
		"no identifier": func(b *model.Branch) { b.ID = id.Nil },
		"no model":      func(b *model.Branch) { b.ModelID = id.Nil },
		"no ordinal":    func(b *model.Branch) { b.Ordinal = "" },
		"no from bus":   func(b *model.Branch) { b.FromBus = "" },
		"no to bus":     func(b *model.Branch) { b.ToBus = "" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := aBranch()
			break_(&got)

			if err := got.Validate(); !errors.Is(err, bserr.ErrInvalid) {
				t.Errorf("Validate() = %v, want ErrInvalid", err)
			}
		})
	}
}

// An alias is allowed to be empty: it is whatever the source called this
// branch, and MATPOWER cases frequently name nothing.
func TestABranchWithNoAliasIsStillValid(t *testing.T) {
	t.Parallel()

	got := aBranch()
	got.Alias = ""

	if err := got.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

// A branch whose two ends are the same bus is not a branch. The importer
// cannot produce one, which is exactly why it is worth refusing here: the
// next importer might.
func TestABranchFromABusToItselfIsInvalid(t *testing.T) {
	t.Parallel()

	got := aBranch()
	got.ToBus = got.FromBus

	if err := got.Validate(); !errors.Is(err, bserr.ErrInvalid) {
		t.Errorf("Validate() = %v, want ErrInvalid", err)
	}
}
