package quantity

import (
	"math"
	"testing"
)

const tol = 1e-9

func closeTo(got, want, tolerance float64) bool {
	return math.Abs(got-want) <= tolerance*math.Max(1, math.Abs(want))
}

// Worked example from any power-systems text: 100 MVA, 138 kV.
//
//	ZBase = 138^2 / 100      = 190.44 ohm
//	IBase = 100e6 / (sqrt3 * 138e3) = 418.37 A
func TestBaseDerivedQuantities(t *testing.T) {
	t.Parallel()

	b := MustNewBase(100, 138)

	if got, want := b.SBase(), 100e6; !closeTo(got, want, tol) {
		t.Errorf("SBase = %v, want %v", got, want)
	}

	if got, want := b.VBase(), 138e3; !closeTo(got, want, tol) {
		t.Errorf("VBase = %v, want %v", got, want)
	}

	if got, want := b.ZBase(), 190.44; !closeTo(got, want, 1e-4) {
		t.Errorf("ZBase = %v ohm, want %v", got, want)
	}

	if got, want := b.IBase(), 418.3697; !closeTo(got, want, 1e-4) {
		t.Errorf("IBase = %v A, want %v", got, want)
	}

	if got, want := b.YBase(), 1/190.44; !closeTo(got, want, 1e-4) {
		t.Errorf("YBase = %v S, want %v", got, want)
	}
}

func TestZBaseAndYBaseAreReciprocal(t *testing.T) {
	t.Parallel()

	for _, b := range []*Base{
		MustNewBase(100, 138),
		MustNewBase(100, 345),
		MustNewBase(1, 0.48),
		MustNewBase(2500, 765),
	} {
		if got := b.ZBase() * b.YBase(); !closeTo(got, 1, 1e-12) {
			t.Errorf("%s: ZBase * YBase = %v, want 1", b, got)
		}
	}
}

// The sqrt(3) in IBase is the three-phase line-to-line convention. Omitting it
// gives a current 73% too high — large enough to be mistaken for a modelling
// error rather than a units error, which is why it is pinned here.
func TestIBaseUsesTheThreePhaseConvention(t *testing.T) {
	t.Parallel()

	b := MustNewBase(100, 138)

	singlePhaseWrong := b.SBase() / b.VBase()
	if closeTo(b.IBase(), singlePhaseWrong, 1e-6) {
		t.Fatal("IBase omits the sqrt(3) three-phase factor")
	}

	if got, want := singlePhaseWrong/b.IBase(), sqrt3; !closeTo(got, want, 1e-12) {
		t.Errorf("ratio to the single-phase form is %v, want sqrt(3) = %v", got, want)
	}
}

func TestNewBaseRejectsNonsense(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		sBase, vBase float64
	}{
		{"zero apparent power", 0, 138},
		{"negative apparent power", -100, 138},
		{"zero voltage", 100, 0},
		{"negative voltage", 100, -138},
		{"infinite apparent power", math.Inf(1), 138},
		{"NaN voltage", 100, math.NaN()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := NewBase(tt.sBase, tt.vBase); err == nil {
				t.Error("expected an error; a degenerate base makes every value " +
					"normalized against it meaningless")
			}
		})
	}
}

func TestScaleForCoversTheElectricalDimensions(t *testing.T) {
	t.Parallel()

	b := MustNewBase(100, 138)

	tests := []struct {
		dim  Dimension
		want float64
	}{
		{Voltage, b.VBase()},
		{Current, b.IBase()},
		{Power, b.SBase()},
		{Impedance, b.ZBase()},
		{Admittance, b.YBase()},
		{Dimensionless, 1},
	}

	for _, tt := range tests {
		got, err := b.scaleFor(tt.dim)
		if err != nil {
			t.Errorf("scaleFor(%s): %v", tt.dim, err)

			continue
		}

		if !closeTo(got, tt.want, tol) {
			t.Errorf("scaleFor(%s) = %v, want %v", tt.dim, got, tt.want)
		}
	}
}

func TestScaleForRejectsDimensionsWithNoBase(t *testing.T) {
	t.Parallel()

	b := MustNewBase(100, 138)

	// Length and temperature are never expressed in per-unit.
	for _, dim := range []Dimension{Length, Temperature, Time, Energy} {
		if _, err := b.scaleFor(dim); err == nil {
			t.Errorf("scaleFor(%s) succeeded; there is no per-unit base for it", dim)
		}
	}
}

func TestEqual(t *testing.T) {
	t.Parallel()

	a := MustNewBase(100, 138)
	same := MustNewBase(100, 138)
	differentVoltage := MustNewBase(100, 345)

	if !a.Equal(same) {
		t.Error("bases with identical values should be equal")
	}

	if a.Equal(differentVoltage) {
		t.Error("bases at different voltage levels should not be equal")
	}

	var nilBase *Base
	if !nilBase.Equal(nil) {
		t.Error("nil should equal nil")
	}

	if a.Equal(nil) || nilBase.Equal(a) {
		t.Error("a real base should not equal nil")
	}
}

func TestStringHandlesNil(t *testing.T) {
	t.Parallel()

	var b *Base
	if got := b.String(); got != "<no base>" {
		t.Errorf("nil String() = %q", got)
	}

	if got, want := MustNewBase(100, 138).String(), "100 MVA / 138 kV"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
