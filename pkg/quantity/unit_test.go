package quantity

import (
	"math"
	"testing"
)

// The two mistakes this package exists to prevent. Both pass dimensional
// analysis and both are wrong.
func TestCompatibleRejectsTheClassicBugs(t *testing.T) {
	t.Parallel()

	t.Run("megawatts are not megavars", func(t *testing.T) {
		t.Parallel()

		if Megawatt.Dim != Megavar.Dim {
			t.Fatal("precondition: active and reactive power share a dimension")
		}

		if Megawatt.Compatible(Megavar) {
			t.Error("MW and MVAr reported compatible; adding them is a real bug " +
				"that dimension alone cannot catch, which is why Kind exists")
		}
	})

	t.Run("per-unit voltage is not kilovolts", func(t *testing.T) {
		t.Parallel()

		if PerUnitVoltage.Dim != Kilovolt.Dim {
			t.Fatal("precondition: both are voltages")
		}

		if PerUnitVoltage.Compatible(Kilovolt) {
			t.Error("pu and kV reported compatible; a per-unit value has no " +
				"absolute meaning without its base")
		}
	})
}

func TestCompatibleAcceptsSameQuantityDifferentScale(t *testing.T) {
	t.Parallel()

	pairs := [][2]Unit{
		{Watt, Megawatt},
		{Volt, Kilovolt},
		{Ampere, Kiloampere},
		{Var, Megavar},
		{VoltAmpere, MegavoltAmpere},
		{Meter, Kilometer},
		{Second, Hour},
		{Radian, Degree},
	}

	for _, p := range pairs {
		if !p[0].Compatible(p[1]) {
			t.Errorf("%s and %s should be compatible", p[0], p[1])
		}
	}
}

func TestCompatibleIsSymmetricAndReflexive(t *testing.T) {
	t.Parallel()

	units := []Unit{
		Volt, Kilovolt, Ampere, Megawatt, Megavar, MegavoltAmpere,
		Ohm, Siemens, Hertz, Meter, Kelvin, Ratio,
		PerUnitVoltage, PerUnitActive, PerUnitImpedance,
	}

	for _, a := range units {
		if !a.Compatible(a) {
			t.Errorf("%s is not compatible with itself", a)
		}

		for _, b := range units {
			if a.Compatible(b) != b.Compatible(a) {
				t.Errorf("Compatible is asymmetric for %s and %s", a, b)
			}
		}
	}
}

// Per-unit units carry the dimension of what they normalize, so dimensional
// checking keeps working inside per-unit arithmetic. Losing this would make
// per-unit values untyped, which defeats the purpose.
func TestPerUnitUnitsCarryTheirDimension(t *testing.T) {
	t.Parallel()

	tests := []struct {
		unit Unit
		dim  Dimension
		kind Kind
	}{
		{PerUnitVoltage, Voltage, KindGeneric},
		{PerUnitCurrent, Current, KindGeneric},
		{PerUnitActive, Power, KindActive},
		{PerUnitReactive, Power, KindReactive},
		{PerUnitApparent, Power, KindApparent},
		{PerUnitImpedance, Impedance, KindGeneric},
		{PerUnitAdmittance, Admittance, KindGeneric},
	}

	for _, tt := range tests {
		if tt.unit.Dim != tt.dim {
			t.Errorf("%s has dimension %s, want %s", tt.unit, tt.unit.Dim, tt.dim)
		}

		if tt.unit.Kind != tt.kind {
			t.Errorf("%s has kind %s, want %s", tt.unit, tt.unit.Kind, tt.kind)
		}

		if !tt.unit.PerUnit {
			t.Errorf("%s is not marked PerUnit", tt.unit)
		}

		if tt.unit.ToSI != 0 {
			t.Errorf("%s has a ToSI factor; per-unit conversion requires a "+
				"Base and a factor here would invite using it", tt.unit)
		}
	}
}

// Per-unit reactive and per-unit active must stay distinguishable, or the
// separation gained in SI units is thrown away as soon as a case is
// normalized — which is where power-flow code spends all its time.
func TestPerUnitPowerFormsStayDistinct(t *testing.T) {
	t.Parallel()

	if PerUnitActive.Compatible(PerUnitReactive) {
		t.Error("per-unit active and reactive power reported compatible")
	}
}

func TestSIUnitFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		dim  Dimension
		kind Kind
		want Unit
	}{
		{Voltage, KindGeneric, Volt},
		{Current, KindGeneric, Ampere},
		{Power, KindActive, Watt},
		{Power, KindReactive, Var},
		{Power, KindApparent, VoltAmpere},
		{Impedance, KindGeneric, Ohm},
		{Admittance, KindGeneric, Siemens},
		{Energy, KindGeneric, Joule},
		{Length, KindGeneric, Meter},
		{Time, KindGeneric, Second},
		{Temperature, KindGeneric, Kelvin},
		{Dimensionless, KindGeneric, Ratio},
	}

	for _, tt := range tests {
		got, err := SIUnitFor(tt.dim, tt.kind)
		if err != nil {
			t.Errorf("SIUnitFor(%s, %s): %v", tt.dim, tt.kind, err)

			continue
		}

		if got != tt.want {
			t.Errorf("SIUnitFor(%s, %s) = %s, want %s", tt.dim, tt.kind, got, tt.want)
		}
	}
}

func TestSIUnitForRejectsUnknownCombination(t *testing.T) {
	t.Parallel()

	// Voltage has no reactive form.
	if _, err := SIUnitFor(Voltage, KindReactive); err == nil {
		t.Error("expected an error for a dimension/kind pair with no SI unit")
	}
}

func TestDegreeToRadianFactor(t *testing.T) {
	t.Parallel()

	// 180 degrees is pi radians. This is the one conversion factor in the
	// package that is not a power of ten, so it is worth checking directly.
	got := 180 * Degree.ToSI
	if math.Abs(got-math.Pi) > 1e-15 {
		t.Errorf("180 deg = %v rad, want %v", got, math.Pi)
	}
}

func TestKindString(t *testing.T) {
	t.Parallel()

	tests := map[Kind]string{
		KindGeneric:  "generic",
		KindActive:   "active",
		KindReactive: "reactive",
		KindApparent: "apparent",
		Kind(99):     "kind(99)",
	}

	for k, want := range tests {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", uint8(k), got, want)
		}
	}
}

func TestUnitString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		unit Unit
		want string
	}{
		{Megawatt, "MW"},
		{Kilovolt, "kV"},
		{Ratio, "1"},
		{PerUnitVoltage, "pu(L^2 M T^-3 I^-1)"},
		{PerUnitActive, "pu(L^2 M T^-3)"},
	}

	for _, tt := range tests {
		if got := tt.unit.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}
