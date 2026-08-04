package conductor_test

import (
	"errors"
	"math"
	"testing"

	"github.com/xraph/blackstart/extensions/hazard/conductor"
	"github.com/xraph/blackstart/pkg/bserr"
)

func TestResistanceInterpolates(t *testing.T) {
	t.Parallel()

	drake, err := conductor.ByCode("drake")
	if err != nil {
		t.Fatalf("ByCode(drake): %v", err)
	}

	if got := drake.ResistanceAt(25); math.Abs(got-drake.ResistanceLowOhmPerM) > 1e-12 {
		t.Errorf("R(25) = %v, want the tabulated low value", got)
	}

	if got := drake.ResistanceAt(75); math.Abs(got-drake.ResistanceHighOhmPerM) > 1e-12 {
		t.Errorf("R(75) = %v, want the tabulated high value", got)
	}

	mid := (drake.ResistanceLowOhmPerM + drake.ResistanceHighOhmPerM) / 2

	if got := drake.ResistanceAt(50); math.Abs(got-mid) > 1e-12 {
		t.Errorf("R(50) = %v, want %v", got, mid)
	}
}

// Extrapolation past the tabulated points is intended, not an accident. A
// conductor above its rated temperature is exactly the case a rating protects
// against, and clamping would make the answer depend on which side of the
// limit the arithmetic landed.
func TestResistanceExtrapolatesPastTheTabulatedRange(t *testing.T) {
	t.Parallel()

	drake, err := conductor.ByCode("drake")
	if err != nil {
		t.Fatalf("ByCode: %v", err)
	}

	if got := drake.ResistanceAt(100); got <= drake.ResistanceHighOhmPerM {
		t.Errorf("R(100) = %v, want above the 75 C value %v",
			got, drake.ResistanceHighOhmPerM)
	}

	if got := drake.ResistanceAt(0); got >= drake.ResistanceLowOhmPerM {
		t.Errorf("R(0) = %v, want below the 25 C value %v",
			got, drake.ResistanceLowOhmPerM)
	}
}

// Resistance rises with temperature for every entry. One with its two
// resistance columns transposed would still interpolate cleanly and be wrong
// at every temperature.
func TestResistanceRisesWithTemperature(t *testing.T) {
	t.Parallel()

	for _, got := range conductor.Library() {
		if got.ResistanceHighOhmPerM <= got.ResistanceLowOhmPerM {
			t.Errorf("%s: R(hot) %v is not above R(cold) %v",
				got.Code, got.ResistanceHighOhmPerM, got.ResistanceLowOhmPerM)
		}
	}
}

func TestEveryLibraryEntryValidates(t *testing.T) {
	t.Parallel()

	for _, got := range conductor.Library() {
		if err := got.Validate(); err != nil {
			t.Errorf("%s: %v", got.Code, err)
		}
	}
}

func TestLibraryCodesAreUnique(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}

	for _, got := range conductor.Library() {
		if seen[got.Code] {
			t.Errorf("%q appears twice; ByCode would return whichever came first",
				got.Code)
		}

		seen[got.Code] = true
	}
}

func TestUnknownCodeIsRefused(t *testing.T) {
	t.Parallel()

	_, err := conductor.ByCode("nosuchconductor")
	if !errors.Is(err, bserr.ErrNotFound) {
		t.Fatalf("ByCode(unknown) error = %v, want bserr.ErrNotFound", err)
	}
}

// Physical plausibility on every row. A transposed digit in a diameter
// produces a rating wrong by a factor and entirely believable — no amount of
// internal consistency would catch it.
func TestLibraryValuesArePhysicallyPlausible(t *testing.T) {
	t.Parallel()

	for _, got := range conductor.Library() {
		if got.DiameterM < 0.005 || got.DiameterM > 0.06 {
			t.Errorf("%s: diameter %v m is not a transmission conductor",
				got.Code, got.DiameterM)
		}

		if got.AreaMM2 < 50 || got.AreaMM2 > 2000 {
			t.Errorf("%s: aluminum area %v mm2 is implausible", got.Code, got.AreaMM2)
		}

		if got.Emissivity <= 0 || got.Emissivity > 1 {
			t.Errorf("%s: emissivity %v is outside 0..1", got.Code, got.Emissivity)
		}

		if got.Absorptivity <= 0 || got.Absorptivity > 1 {
			t.Errorf("%s: absorptivity %v is outside 0..1", got.Code, got.Absorptivity)
		}

		if got.MaxTempC < 50 || got.MaxTempC > 250 {
			t.Errorf("%s: max temperature %v C is implausible", got.Code, got.MaxTempC)
		}

		// Resistance falls as the conductor gets fatter. A row whose area and
		// resistance disagree has had one of them mistyped.
		if got.AreaMM2 > 400 && got.ResistanceLowOhmPerM > 1e-4 {
			t.Errorf("%s: %v mm2 with %v ohm/m — a large conductor with a small "+
				"conductor's resistance", got.Code, got.AreaMM2, got.ResistanceLowOhmPerM)
		}
	}
}

func TestValidateRefusesBrokenConductors(t *testing.T) {
	t.Parallel()

	valid, err := conductor.ByCode("drake")
	if err != nil {
		t.Fatalf("ByCode: %v", err)
	}

	for name, mutate := range map[string]func(*conductor.Conductor){
		"no code":          func(c *conductor.Conductor) { c.Code = "" },
		"no diameter":      func(c *conductor.Conductor) { c.DiameterM = 0 },
		"zero resistance":  func(c *conductor.Conductor) { c.ResistanceLowOhmPerM = 0 },
		"equal temps":      func(c *conductor.Conductor) { c.HighTempC = c.LowTempC },
		"emissivity above": func(c *conductor.Conductor) { c.Emissivity = 1.5 },
		"no max temp":      func(c *conductor.Conductor) { c.MaxTempC = 0 },
	} {
		broken := valid
		mutate(&broken)

		if err := broken.Validate(); err == nil {
			t.Errorf("%s: validated", name)
		}
	}
}

func TestLibraryReturnsAFreshSlice(t *testing.T) {
	t.Parallel()

	got := conductor.Library()
	got[0].Code = "clobbered"

	if conductor.Library()[0].Code == "clobbered" {
		t.Fatal("Library() shares its backing array with its callers")
	}
}
