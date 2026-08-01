package quantity

import (
	"strings"
	"testing"
)

func TestInConvertsWithinAQuantity(t *testing.T) {
	t.Parallel()

	q, err := Megawatts(150).In(Kilowatt)
	if err != nil {
		t.Fatalf("In(kW): %v", err)
	}

	if !closeTo(q.Magnitude(), 150e3, tol) {
		t.Errorf("150 MW = %v kW, want 150000", q.Magnitude())
	}

	if q.Unit() != Kilowatt {
		t.Errorf("unit is %s, want kW", q.Unit())
	}
}

func TestInHandlesTheAffineScale(t *testing.T) {
	t.Parallel()

	back, err := MustNew(298.15, Kelvin).In(Celsius)
	if err != nil {
		t.Fatalf("In(Celsius): %v", err)
	}

	if !closeTo(back.Magnitude(), 25, tol) {
		t.Errorf("298.15 K = %v degC, want 25", back.Magnitude())
	}
}

// The three compatibility conditions fail for different reasons, and the
// message should say which. A dimension mismatch is usually a modelling
// error; a per-unit mismatch is usually a missing conversion. Those get
// debugged in different places.
func TestInRejectsIncompatibleTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		from       Quantity
		to         Unit
		wantReason string
	}{
		{"different dimension", Megawatts(1), Kilovolt, "different dimensions"},
		{"different kind", Megawatts(1), Megavar, "different kinds"},
		{"absolute to per-unit", Kilovolts(138), PerUnitVoltage, "per-unit"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := tt.from.In(tt.to)
			if err == nil {
				t.Fatal("expected an error")
			}

			if !strings.Contains(err.Error(), tt.wantReason) {
				t.Errorf("error %q should explain %q", err, tt.wantReason)
			}
		})
	}
}

func TestPerUnitRoundTrip(t *testing.T) {
	t.Parallel()

	base := MustNewBase(100, 138)
	original := Kilovolts(140.76) // 1.02 pu on a 138 kV base

	pu, err := original.ToPerUnit(PerUnitVoltage, base)
	if err != nil {
		t.Fatalf("ToPerUnit: %v", err)
	}

	if !closeTo(pu.Magnitude(), 1.02, 1e-9) {
		t.Errorf("140.76 kV on a 138 kV base = %v pu, want 1.02", pu.Magnitude())
	}

	back, err := pu.ToAbsolute(Kilovolt)
	if err != nil {
		t.Fatalf("ToAbsolute: %v", err)
	}

	if !closeTo(back.Magnitude(), original.Magnitude(), 1e-9) {
		t.Errorf("round trip gave %v kV, want %v", back.Magnitude(), original.Magnitude())
	}
}

// Applying a base twice is a silent factor error with no symptom other than a
// wrong answer, so each direction refuses to act on a quantity that is already
// in the target form.
func TestConversionsRefuseToActTwice(t *testing.T) {
	t.Parallel()

	base := MustNewBase(100, 138)

	if _, err := Kilovolts(138).ToAbsolute(Volt); err == nil {
		t.Error("ToAbsolute accepted an already-absolute quantity")
	}

	pu, err := NewPerUnit(1.0, PerUnitVoltage, base)
	if err != nil {
		t.Fatalf("NewPerUnit: %v", err)
	}

	if _, err := pu.ToPerUnit(PerUnitVoltage, base); err == nil {
		t.Error("ToPerUnit accepted an already-per-unit quantity")
	}
}

func TestConversionsRequireMatchingTargetForm(t *testing.T) {
	t.Parallel()

	base := MustNewBase(100, 138)

	pu, err := NewPerUnit(1.0, PerUnitVoltage, base)
	if err != nil {
		t.Fatalf("NewPerUnit: %v", err)
	}

	if _, err := pu.ToAbsolute(PerUnitVoltage); err == nil {
		t.Error("ToAbsolute accepted a per-unit target")
	}

	if _, err := Kilovolts(138).ToPerUnit(Kilovolt, base); err == nil {
		t.Error("ToPerUnit accepted an absolute target")
	}
}

func TestToPerUnitRequiresABase(t *testing.T) {
	t.Parallel()

	if _, err := Kilovolts(138).ToPerUnit(PerUnitVoltage, nil); err == nil {
		t.Error("normalized against a nil base")
	}
}

// Crossing dimensions or kinds must fail in both directions, not only in In().
func TestBaseConversionsCheckDimensionAndKind(t *testing.T) {
	t.Parallel()

	base := MustNewBase(100, 138)

	puActive, err := NewPerUnit(0.5, PerUnitActive, base)
	if err != nil {
		t.Fatalf("NewPerUnit: %v", err)
	}

	if _, err := puActive.ToAbsolute(Megavar); err == nil {
		t.Error("converted per-unit active power into megavars")
	}

	if _, err := puActive.ToAbsolute(Kilovolt); err == nil {
		t.Error("converted per-unit power into kilovolts")
	}

	if _, err := Megawatts(50).ToPerUnit(PerUnitReactive, base); err == nil {
		t.Error("normalized megawatts onto a reactive per-unit base")
	}
}
