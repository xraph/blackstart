package quantity

import "testing"

// The point of an exponent vector rather than a dimension enum is that the
// relationships between electrical quantities are *derived*, not asserted. If
// any of these fail, the exponents in dimension.go are wrong.
func TestElectricalRelationshipsFallOutOfTheAlgebra(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  Dimension
		want Dimension
	}{
		{"voltage x current = power", Voltage.Mul(Current), Power},
		{"voltage / current = impedance", Voltage.Div(Current), Impedance},
		{"voltage / impedance = current", Voltage.Div(Impedance), Current},
		{"power / voltage = current", Power.Div(Voltage), Current},
		{"current^2 x impedance = power", Current.Mul(Current).Mul(Impedance), Power},
		{"voltage^2 / impedance = power", Voltage.Mul(Voltage).Div(Impedance), Power},
		{"admittance = 1 / impedance", Impedance.Reciprocal(), Admittance},
		{"impedance = 1 / admittance", Admittance.Reciprocal(), Impedance},
		{"impedance x admittance = dimensionless", Impedance.Mul(Admittance), Dimensionless},
		{"power x time = energy", Power.Mul(Time), Energy},
		{"energy / time = power", Energy.Div(Time), Power},
		{"frequency = 1 / time", Time.Reciprocal(), Frequency},
		{"length / time is not a modelled dimension but composes", Length.Div(Time), Dimension{L: 1, T: -1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.got != tt.want {
				t.Errorf("got %v (%s), want %v (%s)",
					tt.got, tt.got.String(), tt.want, tt.want.String())
			}
		})
	}
}

func TestMulAndDivAreInverse(t *testing.T) {
	t.Parallel()

	for _, d := range []Dimension{
		Dimensionless, Voltage, Current, Power, Impedance, Admittance,
		Energy, Frequency, Temperature, Length,
	} {
		if got := d.Mul(Current).Div(Current); got != d {
			t.Errorf("%s: multiply then divide by current gave %s", d, got)
		}
	}
}

func TestDimensionlessIsTheIdentity(t *testing.T) {
	t.Parallel()

	if got := Voltage.Mul(Dimensionless); got != Voltage {
		t.Errorf("multiplying by dimensionless changed the dimension: %s", got)
	}

	if got := Voltage.Div(Dimensionless); got != Voltage {
		t.Errorf("dividing by dimensionless changed the dimension: %s", got)
	}

	if !Dimensionless.IsDimensionless() {
		t.Error("Dimensionless.IsDimensionless() is false")
	}

	if Voltage.IsDimensionless() {
		t.Error("voltage reported as dimensionless")
	}
}

// Active, reactive and apparent power are dimensionally identical. Adding
// megawatts to megavars is a real bug that dimension alone cannot catch, which
// is why Unit carries a Kind. This test pins the fact so that the reason for
// Kind's existence is not lost.
func TestPowerFormsShareADimension(t *testing.T) {
	t.Parallel()

	if Voltage.Mul(Current) != Power {
		t.Fatal("volt-amperes must share the power dimension")
	}
}

func TestString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		dim  Dimension
		want string
	}{
		{Dimensionless, "1"},
		{Current, "I"},
		{Length, "L"},
		{Power, "L^2 M T^-3"},
		{Voltage, "L^2 M T^-3 I^-1"},
		{Impedance, "L^2 M T^-3 I^-2"},
		{Frequency, "T^-1"},
	}

	for _, tt := range tests {
		if got := tt.dim.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}
