package quantity

import "testing"

// The central invariant: there is no way to reach an SI value from a per-unit
// quantity without a base, because a per-unit quantity without a base cannot
// be constructed in the first place.
func TestPerUnitCannotExistWithoutABase(t *testing.T) {
	t.Parallel()

	if _, err := NewPerUnit(1.02, PerUnitVoltage, nil); err == nil {
		t.Fatal("built a per-unit quantity with a nil base; SI() could then " +
			"only guess, and guessing is what this package exists to prevent")
	}

	// The zero value is dimensionless, not a baseless per-unit value.
	var zero Quantity
	if zero.IsPerUnit() {
		t.Error("the zero Quantity should not be per-unit")
	}

	if _, err := zero.SI(); err != nil {
		t.Errorf("the zero Quantity should convert cleanly: %v", err)
	}
}

func TestNewRejectsPerUnitUnitsAndViceVersa(t *testing.T) {
	t.Parallel()

	if _, err := New(1.02, PerUnitVoltage); err == nil {
		t.Error("New accepted a per-unit unit")
	}

	if _, err := NewPerUnit(230, Kilovolt, MustNewBase(100, 138)); err == nil {
		t.Error("NewPerUnit accepted an absolute unit")
	}
}

func TestNewPerUnitRejectsDimensionsWithNoBase(t *testing.T) {
	t.Parallel()

	// There is no per-unit length, so this must fail at construction rather
	// than at conversion.
	lengthPU := Unit{Symbol: "pu", Dim: Length, PerUnit: true}
	if _, err := NewPerUnit(1, lengthPU, MustNewBase(100, 138)); err == nil {
		t.Error("built a per-unit length; no base scale exists for it")
	}
}

func TestSIForAbsoluteUnits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		q    Quantity
		want float64
	}{
		{"megawatts to watts", Megawatts(150), 150e6},
		{"kilovolts to volts", Kilovolts(138), 138e3},
		{"amperes are already SI", Amperes(420), 420},
		{"megavars to vars", Megavars(-40), -40e6},
		{"kilometers to meters", MustNew(3.5, Kilometer), 3500},
		{"hours to seconds", MustNew(2, Hour), 7200},
		{"percent to ratio", MustNew(5, Percent), 0.05},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.q.SI()
			if err != nil {
				t.Fatalf("SI(): %v", err)
			}

			if !closeTo(got, tt.want, tol) {
				t.Errorf("SI() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Celsius is affine. A ToSI multiplier would make 0 degC convert to 0 K, which
// is wrong by 273.15 and would sail through any test that only checks a
// non-zero value — which is why zero is the first case here.
func TestCelsiusIsAffineNotMultiplicative(t *testing.T) {
	t.Parallel()

	tests := []struct {
		celsius, kelvin float64
	}{
		{0, 273.15},
		{25, 298.15},
		{-40, 233.15},
		{100, 373.15},
	}

	for _, tt := range tests {
		got, err := DegreesC(tt.celsius).SI()
		if err != nil {
			t.Fatalf("SI(): %v", err)
		}

		if !closeTo(got, tt.kelvin, tol) {
			t.Errorf("%g degC = %v K, want %v", tt.celsius, got, tt.kelvin)
		}
	}
}

func TestSIForPerUnit(t *testing.T) {
	t.Parallel()

	base := MustNewBase(100, 138)

	tests := []struct {
		name string
		unit Unit
		mag  float64
		want float64
	}{
		{"per-unit voltage", PerUnitVoltage, 1.02, 1.02 * 138e3},
		{"per-unit active power", PerUnitActive, 0.5, 0.5 * 100e6},
		{"per-unit impedance", PerUnitImpedance, 0.1, 0.1 * base.ZBase()},
		{"per-unit current", PerUnitCurrent, 0.8, 0.8 * base.IBase()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			q, err := NewPerUnit(tt.mag, tt.unit, base)
			if err != nil {
				t.Fatalf("NewPerUnit: %v", err)
			}

			got, err := q.SI()
			if err != nil {
				t.Fatalf("SI(): %v", err)
			}

			if !closeTo(got, tt.want, tol) {
				t.Errorf("SI() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsFinite(t *testing.T) {
	t.Parallel()

	if !Megawatts(1).IsFinite() {
		t.Error("1 MW reported non-finite")
	}

	for _, bad := range []float64{inf(), -inf(), nan()} {
		if Megawatts(bad).IsFinite() {
			t.Errorf("%v reported finite", bad)
		}
	}
}

func TestQuantityString(t *testing.T) {
	t.Parallel()

	base := MustNewBase(100, 138)

	pu, err := NewPerUnit(1.02, PerUnitVoltage, base)
	if err != nil {
		t.Fatalf("NewPerUnit: %v", err)
	}

	tests := []struct {
		q    Quantity
		want string
	}{
		{Megawatts(150), "150 MW"},
		{Kilovolts(138), "138 kV"},
		{Ratios(0.95), "0.95"},
		{pu, "1.02 pu [L^2 M T^-3 I^-1, 100 MVA / 138 kV]"},
	}

	for _, tt := range tests {
		if got := tt.q.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}
