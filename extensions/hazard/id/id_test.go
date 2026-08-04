package id_test

import (
	"testing"

	hid "github.com/xraph/blackstart/extensions/hazard/id"
	"github.com/xraph/blackstart/pkg/id"
)

// Registering from init means a collision with another extension stops the
// process at startup with both owners named.
func TestPrefixesAreRegisteredToThisExtension(t *testing.T) {
	t.Parallel()

	for _, p := range []id.Prefix{hid.Conductor, hid.Assignment, hid.Computation} {
		got, ok := id.Lookup(p)
		if !ok {
			t.Errorf("prefix %q is not registered", p)

			continue
		}

		if got.Owner != hid.Owner {
			t.Errorf("prefix %q is owned by %q, want %q", p, got.Owner, hid.Owner)
		}
	}
}

func TestConstructorsMintTheRightType(t *testing.T) {
	t.Parallel()

	if got := hid.NewConductor().Prefix(); got != hid.Conductor {
		t.Errorf("NewConductor minted a %q identifier", got)
	}

	if got := hid.NewAssignment().Prefix(); got != hid.Assignment {
		t.Errorf("NewAssignment minted a %q identifier", got)
	}

	if got := hid.NewComputation().Prefix(); got != hid.Computation {
		t.Errorf("NewComputation minted a %q identifier", got)
	}
}

// An assignment identifier handed to a computation lookup should fail as
// "wrong type" rather than later as "not found".
func TestParsersRejectTheOtherTypes(t *testing.T) {
	t.Parallel()

	conductor := hid.NewConductor()
	assignment := hid.NewAssignment()
	computation := hid.NewComputation()

	for _, tt := range []struct {
		name  string
		parse func(string) (id.ID, error)
		own   string
		other []string
	}{
		{
			"conductor", hid.ParseConductor, conductor.String(),
			[]string{assignment.String(), computation.String()},
		},
		{
			"assignment", hid.ParseAssignment, assignment.String(),
			[]string{conductor.String(), computation.String()},
		},
		{
			"computation", hid.ParseComputation, computation.String(),
			[]string{conductor.String(), assignment.String()},
		},
	} {
		if _, err := tt.parse(tt.own); err != nil {
			t.Errorf("Parse%s rejected its own identifier: %v", tt.name, err)
		}

		for _, other := range tt.other {
			if _, err := tt.parse(other); err == nil {
				t.Errorf("Parse%s accepted %q", tt.name, other)
			}
		}
	}
}

// Prefixes are immutable once shipped: changing one invalidates every stored
// identifier and every series key built from it.
func TestPrefixValuesAreFrozen(t *testing.T) {
	t.Parallel()

	if hid.Conductor != "hzcnd" || hid.Assignment != "hzasg" ||
		hid.Computation != "hzcmp" {
		t.Errorf("a shipped prefix changed: %q %q %q",
			hid.Conductor, hid.Assignment, hid.Computation)
	}
}
