package quantity

import (
	"strings"
	"testing"
)

func TestAddConvertsToTheReceiversUnit(t *testing.T) {
	t.Parallel()

	got, err := Megawatts(1).Add(MustNew(500, Kilowatt))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if got.Unit() != Megawatt {
		t.Errorf("result unit is %s, want MW", got.Unit())
	}

	if !closeTo(got.Magnitude(), 1.5, tol) {
		t.Errorf("1 MW + 500 kW = %v MW, want 1.5", got.Magnitude())
	}
}

// The headline case. Both are kg m^2 s^-3 and both are "power"; the sum is
// meaningless and must not compile into a number.
func TestAddRejectsMegawattsPlusMegavars(t *testing.T) {
	t.Parallel()

	_, err := Megawatts(100).Add(Megavars(40))
	if err == nil {
		t.Fatal("summed active and reactive power")
	}

	if !strings.Contains(err.Error(), "different kinds") {
		t.Errorf("error should name the kind mismatch, got %q", err)
	}
}

func TestAddRejectsPerUnitAndAbsolute(t *testing.T) {
	t.Parallel()

	pu, err := NewPerUnit(1.0, PerUnitVoltage, MustNewBase(100, 138))
	if err != nil {
		t.Fatalf("NewPerUnit: %v", err)
	}

	if _, err := pu.Add(Kilovolts(138)); err == nil {
		t.Error("added a per-unit voltage to an absolute one")
	}
}

// Two per-unit voltages on different voltage levels are different physical
// quantities that happen to be written the same way.
func TestAddRejectsDifferentBases(t *testing.T) {
	t.Parallel()

	at138, err := NewPerUnit(1.0, PerUnitVoltage, MustNewBase(100, 138))
	if err != nil {
		t.Fatalf("NewPerUnit: %v", err)
	}

	at345, err := NewPerUnit(1.0, PerUnitVoltage, MustNewBase(100, 345))
	if err != nil {
		t.Fatalf("NewPerUnit: %v", err)
	}

	_, err = at138.Add(at345)
	if err == nil {
		t.Fatal("summed per-unit voltages from different voltage levels")
	}

	if !strings.Contains(err.Error(), "different bases") {
		t.Errorf("error should name the base mismatch, got %q", err)
	}
}

func TestSub(t *testing.T) {
	t.Parallel()

	got, err := Megawatts(100).Sub(Megawatts(30))
	if err != nil {
		t.Fatalf("Sub: %v", err)
	}

	if !closeTo(got.Magnitude(), 70, tol) {
		t.Errorf("100 - 30 = %v MW, want 70", got.Magnitude())
	}
}

// Comparing raw magnitudes across units is a real bug: 900 kW is not greater
// than 1 MW, but 900 > 1 is.
func TestCmpConvertsBeforeComparing(t *testing.T) {
	t.Parallel()

	flow := MustNew(900, Kilowatt)
	limit := Megawatts(1)

	got, err := flow.Cmp(limit)
	if err != nil {
		t.Fatalf("Cmp: %v", err)
	}

	if got != -1 {
		t.Errorf("900 kW vs 1 MW = %d, want -1; comparing bare magnitudes "+
			"would have said 900 > 1", got)
	}
}

func TestCmpOrdering(t *testing.T) {
	t.Parallel()

	tests := []struct {
		a, b Quantity
		want int
	}{
		{Megawatts(1), Megawatts(2), -1},
		{Megawatts(2), Megawatts(1), 1},
		{Megawatts(1), Megawatts(1), 0},
		{Megawatts(-5), Megawatts(0), -1},
		{MustNew(1000, Kilowatt), Megawatts(1), 0},
	}

	for _, tt := range tests {
		got, err := tt.a.Cmp(tt.b)
		if err != nil {
			t.Fatalf("Cmp: %v", err)
		}

		if got != tt.want {
			t.Errorf("%s cmp %s = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestEqualUsesRelativeTolerance(t *testing.T) {
	t.Parallel()

	// A round trip through per-unit will not return the exact bits.
	base := MustNewBase(100, 138)

	pu, err := Kilovolts(138).ToPerUnit(PerUnitVoltage, base)
	if err != nil {
		t.Fatalf("ToPerUnit: %v", err)
	}

	back, err := pu.ToAbsolute(Kilovolt)
	if err != nil {
		t.Fatalf("ToAbsolute: %v", err)
	}

	eq, err := Kilovolts(138).Equal(back, 1e-12)
	if err != nil {
		t.Fatalf("Equal: %v", err)
	}

	if !eq {
		t.Errorf("round trip differs by more than 1e-12: %s vs 138 kV", back)
	}

	// Zero compares exactly.
	eq, err = Megawatts(0).Equal(Megawatts(0), 0)
	if err != nil {
		t.Fatalf("Equal: %v", err)
	}

	if !eq {
		t.Error("zero should equal zero")
	}
}

func TestScaleNegAbs(t *testing.T) {
	t.Parallel()

	q := Megawatts(120)

	if got := q.Scale(0.5); !closeTo(got.Magnitude(), 60, tol) {
		t.Errorf("Scale(0.5) = %v, want 60", got.Magnitude())
	}

	if got := q.Neg(); !closeTo(got.Magnitude(), -120, tol) {
		t.Errorf("Neg() = %v, want -120", got.Magnitude())
	}

	if got := Megawatts(-45).Abs(); !closeTo(got.Magnitude(), 45, tol) {
		t.Errorf("Abs() = %v, want 45", got.Magnitude())
	}

	// The unit and base must survive all three.
	base := MustNewBase(100, 138)

	pu, err := NewPerUnit(0.8, PerUnitActive, base)
	if err != nil {
		t.Fatalf("NewPerUnit: %v", err)
	}

	for name, got := range map[string]Quantity{
		"Scale": pu.Scale(2),
		"Neg":   pu.Neg(),
		"Abs":   pu.Abs(),
	} {
		if got.Unit() != PerUnitActive {
			t.Errorf("%s dropped the unit", name)
		}

		if !got.Base().Equal(base) {
			t.Errorf("%s dropped the base", name)
		}
	}
}

func TestSum(t *testing.T) {
	t.Parallel()

	got, err := Sum([]Quantity{
		Megawatts(10),
		MustNew(5000, Kilowatt),
		Megawatts(2.5),
	})
	if err != nil {
		t.Fatalf("Sum: %v", err)
	}

	if !closeTo(got.Magnitude(), 17.5, tol) {
		t.Errorf("sum = %v MW, want 17.5", got.Magnitude())
	}
}

// A zero of unknown unit silently entering a megawatt calculation is exactly
// the failure this package exists to stop, so an empty sum is an error.
func TestSumRejectsAnEmptySlice(t *testing.T) {
	t.Parallel()

	if _, err := Sum(nil); err == nil {
		t.Error("summed an empty slice; the unit of the result is unknowable")
	}
}

func TestSumReportsWhichElementFailed(t *testing.T) {
	t.Parallel()

	_, err := Sum([]Quantity{Megawatts(1), Megawatts(2), Megavars(3)})
	if err == nil {
		t.Fatal("expected an error")
	}

	if !strings.Contains(err.Error(), "element 2") {
		t.Errorf("error should identify the offending index, got %q", err)
	}
}
