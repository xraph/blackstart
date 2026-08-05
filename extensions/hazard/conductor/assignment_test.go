package conductor_test

import (
	"testing"

	"github.com/xraph/blackstart/extensions/hazard/conductor"
	hid "github.com/xraph/blackstart/extensions/hazard/id"
	"github.com/xraph/blackstart/pkg/id"
)

// Every default must exist in the library. A default naming a conductor that
// was renamed or dropped fails at compute time, a long way from the edit.
func TestEveryVoltageDefaultIsInTheLibrary(t *testing.T) {
	t.Parallel()

	for _, kv := range []float64{69, 115, 138, 161, 230, 345, 500, 765} {
		code, err := conductor.AssumeByVoltage(kv)
		if err != nil {
			t.Errorf("AssumeByVoltage(%v): %v", kv, err)

			continue
		}

		if _, err := conductor.ByCode(code); err != nil {
			t.Errorf("AssumeByVoltage(%v) = %q, which is not in the library", kv, code)
		}
	}
}

// A higher voltage never gets a smaller conductor. The thresholds are a
// judgement call, but this direction is not: a 500 kV line assumed to be thinner
// than a 115 kV one would produce a rating nobody would think to question.
func TestVoltageDefaultsAreMonotone(t *testing.T) {
	t.Parallel()

	previous := 0.0

	for _, kv := range []float64{69, 115, 138, 161, 199, 200, 230, 299, 300, 345, 500, 765} {
		code, err := conductor.AssumeByVoltage(kv)
		if err != nil {
			t.Fatalf("AssumeByVoltage(%v): %v", kv, err)
		}

		got, err := conductor.ByCode(code)
		if err != nil {
			t.Fatalf("ByCode(%q): %v", code, err)
		}

		if got.AreaMM2 < previous {
			t.Errorf("AssumeByVoltage(%v) = %q at %v mm2, smaller than the %v mm2 "+
				"assumed for the voltage below it", kv, code, got.AreaMM2, previous)
		}

		previous = got.AreaMM2
	}
}

func TestVoltageMustBePositive(t *testing.T) {
	t.Parallel()

	for _, kv := range []float64{0, -138} {
		if _, err := conductor.AssumeByVoltage(kv); err == nil {
			t.Errorf("AssumeByVoltage(%v) returned a conductor", kv)
		}
	}
}

// A surveyed assignment and an assumed one are different claims, and the
// difference has to be on the row rather than inferred — a rating conditioned
// on a guess is not the same statement as one conditioned on a fact.
func TestBasisIsRequired(t *testing.T) {
	t.Parallel()

	assignment := conductor.Assignment{
		ID:            hid.NewAssignment(),
		CarrierID:     id.MustNew("brch"),
		ConductorCode: "drake",
	}

	if err := assignment.Validate(); err == nil {
		t.Fatal("an assignment with no basis validated")
	}
}

func TestValidAssignmentValidates(t *testing.T) {
	t.Parallel()

	assignment := conductor.Assignment{
		ID:            hid.NewAssignment(),
		CarrierID:     id.MustNew("brch"),
		ConductorCode: "drake",
		Basis:         conductor.Surveyed,
	}

	if err := assignment.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestAssignmentRejectsBadRows(t *testing.T) {
	t.Parallel()

	valid := conductor.Assignment{
		ID:            hid.NewAssignment(),
		CarrierID:     id.MustNew("brch"),
		ConductorCode: "drake",
		Basis:         conductor.Surveyed,
	}

	for name, mutate := range map[string]func(*conductor.Assignment){
		"no id":           func(a *conductor.Assignment) { a.ID = id.Nil },
		"no carrier":      func(a *conductor.Assignment) { a.CarrierID = id.Nil },
		"no code":         func(a *conductor.Assignment) { a.ConductorCode = "" },
		"unknown basis":   func(a *conductor.Assignment) { a.Basis = "vibes" },
		"unknown code":    func(a *conductor.Assignment) { a.ConductorCode = "drakee" },
		"wrong id prefix": func(a *conductor.Assignment) { a.ID = id.MustNew("span") },
	} {
		broken := valid
		mutate(&broken)

		if err := broken.Validate(); err == nil {
			t.Errorf("%s: validated", name)
		}
	}
}

func TestEveryBasisIsValid(t *testing.T) {
	t.Parallel()

	if len(conductor.Bases()) < 2 {
		t.Fatal("a basis vocabulary with fewer than two entries records nothing")
	}

	for _, basis := range conductor.Bases() {
		if !basis.Valid() {
			t.Errorf("%q is in Bases() but is not Valid()", basis)
		}
	}

	if conductor.Basis("").Valid() {
		t.Error("the empty basis is Valid()")
	}
}
