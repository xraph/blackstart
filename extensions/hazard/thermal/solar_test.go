package thermal_test

import (
	"math"
	"testing"
	"time"

	"github.com/xraph/blackstart/extensions/hazard/thermal"
)

// elevenAM is 11:00 apparent solar time at 90 degrees west, which is the hour
// IEEE 738-2012's worked example is set at. Every test in this package uses it,
// so that the sun is always in the standard's sky.
func elevenAM() time.Time { return time.Date(2026, 6, 10, 17, 0, 0, 0, time.UTC) }

// midnight is the same longitude twelve hours round.
func midnight() time.Time { return time.Date(2026, 6, 10, 6, 0, 0, 0, time.UTC) }

// The sun's altitude at the worked example's place and time, checked on its own
// because it feeds the solar term and nothing else does. The standard reports
// 74.9 degrees.
func TestSolarAltitudeMatchesTheWorkedExample(t *testing.T) {
	t.Parallel()

	got := thermal.SolarAltitudeDeg(30, -90, elevenAM())

	if math.Abs(got-74.9) > 1 {
		t.Errorf("solar altitude = %.2f degrees, want about 74.9", got)
	}
}

// And the flux at that altitude, which the standard tabulates at about
// 1027 W/m2 for a clear sky.
func TestSolarFluxMatchesTheWorkedExample(t *testing.T) {
	t.Parallel()

	got := thermal.SolarFlux(30, -90, elevenAM(), 0)

	if math.Abs(got-1027) > 30 {
		t.Errorf("solar flux = %.1f W/m2, want about 1027", got)
	}
}

// The sun is below the horizon at midnight and the flux is zero, not a
// negative number the polynomial happens to produce there.
func TestNoFluxAtNight(t *testing.T) {
	t.Parallel()

	if got := thermal.SolarAltitudeDeg(30, -90, midnight()); got >= 0 {
		t.Errorf("solar altitude = %.2f degrees at midnight", got)
	}

	if got := thermal.SolarFlux(30, -90, midnight(), 0); got != 0 {
		t.Errorf("solar flux = %v at midnight", got)
	}
}

// Longitude moves the sun. The same instant is noon somewhere and dawn
// somewhere else, and a model that ignored longitude would put the sun in the
// same place for the whole hemisphere.
func TestLongitudeMovesTheSun(t *testing.T) {
	t.Parallel()

	west := thermal.SolarAltitudeDeg(30, -90, elevenAM())
	east := thermal.SolarAltitudeDeg(30, 0, elevenAM())

	if math.Abs(west-east) < 10 {
		t.Errorf("six time zones apart the sun barely moved: %.2f and %.2f degrees",
			west, east)
	}
}

// June is summer in the north and winter in the south. A declination with its
// sign inverted would put the sun high in both, and every rating south of the
// equator would be wrong by a season.
func TestTheHemispheresDisagreeAboutJune(t *testing.T) {
	t.Parallel()

	north := thermal.SolarAltitudeDeg(45, -90, elevenAM())
	south := thermal.SolarAltitudeDeg(-45, -90, elevenAM())

	if north <= south {
		t.Errorf("in June the southern sun is no lower: %.2f north, %.2f south",
			north, south)
	}
}

// Cloud only ever takes flux away, and full overcast takes most of it.
func TestCloudAttenuates(t *testing.T) {
	t.Parallel()

	previous := math.Inf(1)

	for _, cover := range []float64{0, 25, 50, 75, 100} {
		got := thermal.SolarFlux(30, -90, elevenAM(), cover)

		if got > previous {
			t.Errorf("%v%% cloud let through %.1f W/m2, more than the %.1f below it",
				cover, got, previous)
		}

		if got <= 0 {
			t.Errorf("%v%% cloud put the sun out entirely", cover)
		}

		previous = got
	}
}
