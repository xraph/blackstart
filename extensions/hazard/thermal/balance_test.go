package thermal_test

import (
	"math"
	"testing"

	"github.com/xraph/blackstart/extensions/hazard/thermal"
)

// The worked example's conditions, as a starting point each test perturbs one
// term of. The absolute answer is [TestMatchesTheIEEE738WorkedExample]'s job;
// these check the directions, which no single spot value can.
func nominal(t *testing.T) thermal.RatingInput {
	t.Helper()

	return thermal.RatingInput{
		Conductor: drake(t), AmbientK: 313.15, WindMS: 0.61, WindAngleDeg: 90,
		LatitudeDeg: 30, LongitudeDeg: -90, At: elevenAM(),
	}
}

func rate(t *testing.T, in thermal.RatingInput) thermal.Rating {
	t.Helper()

	got, err := thermal.Ampacity(in)
	if err != nil {
		t.Fatalf("Ampacity: %v", err)
	}

	return got
}

// A rating computed after dark with a daytime flux would be low by around
// fifteen percent and look entirely reasonable.
func TestALineCarriesMoreAtNight(t *testing.T) {
	t.Parallel()

	day := rate(t, nominal(t))

	dark := nominal(t)
	dark.At = midnight()

	night := rate(t, dark)

	if night.SolarWPerM != 0 {
		t.Errorf("solar heating = %v W/m at midnight", night.SolarWPerM)
	}

	if night.AmpacityA <= day.AmpacityA {
		t.Errorf("the same line carries no more at night: %.1f A then %.1f A",
			day.AmpacityA, night.AmpacityA)
	}
}

// Ampacity falls as the ambient rises. A single spot value cannot catch a sign
// error in the heat balance; a sweep can.
func TestAmpacityFallsAsAmbientRises(t *testing.T) {
	t.Parallel()

	previous := math.Inf(1)

	for _, ambientC := range []float64{0, 20, 40} {
		in := nominal(t)
		in.AmbientK = ambientC + 273.15

		got := rate(t, in)

		if got.AmpacityA >= previous {
			t.Errorf("%v C gave %.1f A, no less than the %.1f A below it",
				ambientC, got.AmpacityA, previous)
		}

		previous = got.AmpacityA
	}
}

// More wind is more cooling is more current. This is the whole reason a dynamic
// rating beats a static one, so it had better hold.
func TestMoreWindRaisesAmpacity(t *testing.T) {
	t.Parallel()

	previous := 0.0

	for _, wind := range []float64{0.61, 2, 5, 10} {
		in := nominal(t)
		in.WindMS = wind

		got := rate(t, in)

		if got.AmpacityA <= previous {
			t.Errorf("%v m/s gave %.1f A, no more than the %.1f A below it",
				wind, got.AmpacityA, previous)
		}

		previous = got.AmpacityA
	}
}

// Wind across a conductor cools it better than wind along it, which is the
// whole content of the direction factor.
func TestCrosswindCoolsBetterThanWindAlongTheConductor(t *testing.T) {
	t.Parallel()

	in := nominal(t)
	in.WindMS = 5

	in.WindAngleDeg = 90
	across := rate(t, in)

	in.WindAngleDeg = 0
	along := rate(t, in)

	if across.AmpacityA <= along.AmpacityA {
		t.Errorf("crosswind did not cool better: %.1f A across, %.1f A along",
			across.AmpacityA, along.AmpacityA)
	}
}

// A conductor does not know which end of itself the wind came from.
func TestWindAngleFoldsIntoAQuarterTurn(t *testing.T) {
	t.Parallel()

	in := nominal(t)
	in.WindMS = 5

	in.WindAngleDeg = 30
	acute := rate(t, in)

	in.WindAngleDeg = 210
	reflex := rate(t, in)

	if math.Abs(acute.AmpacityA-reflex.AmpacityA) > 1e-9 {
		t.Errorf("30 and 210 degrees disagreed: %.4f A then %.4f A",
			acute.AmpacityA, reflex.AmpacityA)
	}
}

// Cloud shades the conductor, so an overcast line carries more.
func TestCloudRaisesAmpacity(t *testing.T) {
	t.Parallel()

	sunny := rate(t, nominal(t))

	shaded := nominal(t)
	shaded.CloudCoverPercent = 100

	overcast := rate(t, shaded)

	if overcast.AmpacityA <= sunny.AmpacityA {
		t.Errorf("cloud did not raise ampacity: %.1f A sunny, %.1f A overcast",
			sunny.AmpacityA, overcast.AmpacityA)
	}
}

// Still air still carries current. Natural convection and radiation do not
// stop, and a model that returned zero here would be reading dead calm as a
// total loss of the line.
func TestStillAirStillRates(t *testing.T) {
	t.Parallel()

	in := nominal(t)
	in.WindMS = 0
	in.At = midnight()

	got := rate(t, in)

	if got.AmpacityA <= 0 {
		t.Fatalf("still air rated at %v A", got.AmpacityA)
	}

	// Natural convection on Drake at 60 degrees of rise is around 42 W/m. A
	// forced correlation evaluated at zero wind gives zero, and taking the
	// largest of the three is what keeps that from happening.
	if got.ConvectiveWPerM < 30 || got.ConvectiveWPerM > 60 {
		t.Errorf("still-air convection = %.1f W/m, not natural convection",
			got.ConvectiveWPerM)
	}
}

// An ambient at or above the conductor's limit leaves no headroom, and the
// answer is zero rather than an error: it is a real statement about a real
// afternoon, and it is exactly what a hazard model exists to say.
func TestNoHeadroomRatesZero(t *testing.T) {
	t.Parallel()

	in := nominal(t)
	in.AmbientK = in.Conductor.MaxTempC + 273.15 + 5

	if got := rate(t, in); got.AmpacityA != 0 {
		t.Errorf("ampacity = %v A above the conductor's limit", got.AmpacityA)
	}
}

// Thin air both cools less and lets more sun through, and the first effect
// wins: a line at altitude is rated lower than the same line at sea level.
func TestElevationLowersAmpacity(t *testing.T) {
	t.Parallel()

	sea := rate(t, nominal(t))

	high := nominal(t)
	high.ElevationM = 2000

	mountain := rate(t, high)

	if mountain.AmpacityA >= sea.AmpacityA {
		t.Errorf("2000 m rated no lower than sea level: %.1f A then %.1f A",
			sea.AmpacityA, mountain.AmpacityA)
	}
}
