package mechanical_test

import (
	"math"
	"testing"

	"github.com/xraph/blackstart/extensions/hazard/mechanical"
)

const iceDensity = 917.0

// Above freezing, precipitation is rain. A model that iced a warm storm would
// be wrong in the direction that matters most — a fabricated load on a line
// nobody needs to derate.
func TestNoAccretionAboveFreezing(t *testing.T) {
	t.Parallel()

	got, err := mechanical.Ice(mechanical.IceInput{
		FreezingRainKgPerM2: 5, TempK: 280, WindMS: 10, DiameterM: 0.028,
	})
	if err != nil {
		t.Fatalf("Ice: %v", err)
	}

	if got.ThicknessM != 0 || got.MassKgPerM != 0 {
		t.Errorf("iced at 280 K: %+v", got)
	}
}

// And exactly at freezing it still ices. The boundary belongs on the accreting
// side: freezing rain is by definition liquid arriving at a surface at or below
// freezing, and a strict comparison would drop the whole isotherm.
func TestFreezingPointStillAccretes(t *testing.T) {
	t.Parallel()

	got, err := mechanical.Ice(mechanical.IceInput{
		FreezingRainKgPerM2: 5, TempK: 273.15, WindMS: 10, DiameterM: 0.028,
	})
	if err != nil {
		t.Fatalf("Ice: %v", err)
	}

	if got.ThicknessM <= 0 {
		t.Errorf("no ice at exactly 273.15 K: %+v", got)
	}
}

// The returned thickness and mass must describe the same sleeve. Two fields
// that can disagree are two fields that eventually will.
func TestThicknessAndMassAgree(t *testing.T) {
	t.Parallel()

	got, err := mechanical.Ice(mechanical.IceInput{
		FreezingRainKgPerM2: 8, TempK: 270, WindMS: 12, DiameterM: 0.028,
	})
	if err != nil {
		t.Fatalf("Ice: %v", err)
	}

	want := iceDensity * math.Pi * got.ThicknessM * (0.028 + got.ThicknessM)

	if math.Abs(got.MassKgPerM-want) > 1e-9 {
		t.Errorf("mass %v does not match the sleeve its thickness implies (%v)",
			got.MassKgPerM, want)
	}
}

func TestNoPrecipitationIsNoIce(t *testing.T) {
	t.Parallel()

	got, err := mechanical.Ice(mechanical.IceInput{
		FreezingRainKgPerM2: 0, TempK: 265, WindMS: 15, DiameterM: 0.028,
	})
	if err != nil {
		t.Fatalf("Ice: %v", err)
	}

	if got.ThicknessM != 0 || got.MassKgPerM != 0 {
		t.Errorf("iced with no precipitation: %+v", got)
	}
}

func TestThicknessRisesWithAccumulation(t *testing.T) {
	t.Parallel()

	previous := 0.0

	for _, accumulation := range []float64{1, 2, 5, 10, 25} {
		got, err := mechanical.Ice(mechanical.IceInput{
			FreezingRainKgPerM2: accumulation, TempK: 270,
			WindMS: 10, DiameterM: 0.028,
		})
		if err != nil {
			t.Fatalf("Ice: %v", err)
		}

		if got.ThicknessM <= previous {
			t.Errorf("%v kg/m2 gave %v m, no more than the %v m below it",
				accumulation, got.ThicknessM, previous)
		}

		previous = got.ThicknessM
	}
}

// Wind drives droplets onto the side of the conductor as well as the top, so
// the same accumulation collects more ice in a gale. This is the collection
// term, and without it a wind-driven ice storm reads like a calm one.
func TestWindRaisesCollection(t *testing.T) {
	t.Parallel()

	calm := mechanical.IceInput{
		FreezingRainKgPerM2: 8, PrecipRateKgPerM2S: 5e-4, TempK: 270,
		WindMS: 0, DiameterM: 0.028,
	}

	windy := calm
	windy.WindMS = 20

	still, err := mechanical.Ice(calm)
	if err != nil {
		t.Fatalf("Ice: %v", err)
	}

	blown, err := mechanical.Ice(windy)
	if err != nil {
		t.Fatalf("Ice: %v", err)
	}

	if blown.ThicknessM <= still.ThicknessM {
		t.Errorf("wind did not raise collection: %v m still, %v m at 20 m/s",
			still.ThicknessM, blown.ThicknessM)
	}
}

// A quarter-inch radial glaze on a Drake conductor is the classic damaging ice
// storm, and the accumulation that produces it is a few kilograms per square
// meter. If this ever reads a millimeter or a foot, the geometry has inverted.
func TestAnIceStormIsTheRightOrderOfMagnitude(t *testing.T) {
	t.Parallel()

	got, err := mechanical.Ice(mechanical.IceInput{
		FreezingRainKgPerM2: 12, TempK: 270, WindMS: 0, DiameterM: 0.02814,
	})
	if err != nil {
		t.Fatalf("Ice: %v", err)
	}

	if got.ThicknessM < 0.002 || got.ThicknessM > 0.03 {
		t.Errorf("thickness = %v m, not the radial glaze a 12 kg/m2 freezing "+
			"rain event puts on a conductor", got.ThicknessM)
	}

	if got.MassKgPerM < 0.2 || got.MassKgPerM > 5 {
		t.Errorf("mass = %v kg/m, not the loading an ice storm adds",
			got.MassKgPerM)
	}
}

func TestIceRefusesImplausibleInput(t *testing.T) {
	t.Parallel()

	for name, in := range map[string]mechanical.IceInput{
		"no diameter":       {FreezingRainKgPerM2: 5, TempK: 270},
		"negative rain":     {FreezingRainKgPerM2: -1, TempK: 270, DiameterM: 0.028},
		"negative rate":     {PrecipRateKgPerM2S: -1, TempK: 270, DiameterM: 0.028},
		"negative wind":     {FreezingRainKgPerM2: 5, TempK: 270, WindMS: -3, DiameterM: 0.028},
		"below absolute 0":  {FreezingRainKgPerM2: 5, TempK: -1, DiameterM: 0.028},
		"absolute zero air": {FreezingRainKgPerM2: 5, TempK: 0, DiameterM: 0.028},
	} {
		if _, err := mechanical.Ice(in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// Ice hangs down, wind pushes sideways. The combined load is the vector, and
// its magnitude exceeds either part.
func TestCombinedComposesAsAVector(t *testing.T) {
	t.Parallel()

	got := mechanical.Combined(
		mechanical.WindLoad{DragNPerM: 30},
		mechanical.IceLoad{MassKgPerM: 4},
	)

	if got.HorizontalNPerM != 30 {
		t.Errorf("horizontal = %v, want the drag", got.HorizontalNPerM)
	}

	// Weight is mass times g, not mass.
	if math.Abs(got.VerticalNPerM-4*9.80665) > 1e-6 {
		t.Errorf("vertical = %v, want the ice weight", got.VerticalNPerM)
	}

	if got.MagnitudeNPerM <= got.HorizontalNPerM ||
		got.MagnitudeNPerM <= got.VerticalNPerM {
		t.Errorf("magnitude %v does not exceed both components", got.MagnitudeNPerM)
	}

	if got.AngleDeg <= 0 || got.AngleDeg >= 90 {
		t.Errorf("angle = %v, want a swing between straight down and horizontal",
			got.AngleDeg)
	}
}

// The angle is measured from straight down, so ice with no wind hangs at zero
// and wind with no ice blows out to ninety. Getting this reversed produces a
// number that is plausible at every intermediate value.
func TestCombinedAngleIsMeasuredFromVertical(t *testing.T) {
	t.Parallel()

	hanging := mechanical.Combined(
		mechanical.WindLoad{}, mechanical.IceLoad{MassKgPerM: 4})
	if hanging.AngleDeg != 0 {
		t.Errorf("angle = %v with no wind, want 0", hanging.AngleDeg)
	}

	blowing := mechanical.Combined(mechanical.WindLoad{DragNPerM: 30}, mechanical.IceLoad{})
	if math.Abs(blowing.AngleDeg-90) > 1e-9 {
		t.Errorf("angle = %v with no ice, want 90", blowing.AngleDeg)
	}

	// Equal parts swing to forty-five.
	even := mechanical.Combined(
		mechanical.WindLoad{DragNPerM: 4 * 9.80665},
		mechanical.IceLoad{MassKgPerM: 4})
	if math.Abs(even.AngleDeg-45) > 1e-9 {
		t.Errorf("angle = %v on equal components, want 45", even.AngleDeg)
	}
}

func TestCombinedOfNothingIsNothing(t *testing.T) {
	t.Parallel()

	got := mechanical.Combined(mechanical.WindLoad{}, mechanical.IceLoad{})

	if got.MagnitudeNPerM != 0 || got.AngleDeg != 0 {
		t.Errorf("an unloaded conductor: %+v", got)
	}
}
