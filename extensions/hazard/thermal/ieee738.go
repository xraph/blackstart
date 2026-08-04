package thermal

import (
	"fmt"
	"math"
	"time"

	"github.com/xraph/blackstart/extensions/hazard/conductor"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/quantity"
)

// Constants of the heat balance, all from IEEE Std 738-2012 in SI.
const (
	// Air viscosity, after Sutherland: μ = 1.458e-6·T^1.5 / (T + 110.4), with T
	// in kelvin. The standard writes the same thing in celsius with 383.4 in
	// the denominator, which is 273 + 110.4.
	viscosityCoefficient = 1.458e-6
	sutherlandK          = 110.4

	// Air density at sea level and 0 C, thinned with elevation and warmth.
	densityAtSeaLevel     = 1.293
	densityPerMeter       = -1.525e-4
	densityPerMeterSq     = 6.379e-9
	densityThermalPerDegC = 0.00367

	// Thermal conductivity of air against film temperature in celsius.
	conductivityA = 2.424e-2
	conductivityB = 7.477e-5
	conductivityC = -4.407e-9

	// Forced convection, the low-wind and high-wind correlations.
	lowWindOffset    = 1.01
	lowWindFactor    = 1.35
	lowWindExponent  = 0.52
	highWindFactor   = 0.754
	highWindExponent = 0.6

	// Natural convection, for when the forced correlations fall below it.
	naturalFactor           = 3.645
	naturalDensityExponent  = 0.5
	naturalDiameterExponent = 0.75
	naturalDeltaExponent    = 1.25

	// Wind direction factor, which falls to about 0.39 for wind blowing along
	// the conductor rather than across it.
	angleOffset  = 1.194
	angleCosTwo  = 0.194
	angleSinTwo  = 0.368
	quarterTurn  = 90
	radiationK   = 273
	radiationDiv = 100

	// Radiation, with the Stefan-Boltzmann constant and the per-meter cylinder
	// area folded into one figure.
	radiationFactor = 17.8

	zeroCelsiusK = 273.15
)

// RatingInput is one conductor and the weather standing around it.
type RatingInput struct {
	// Conductor is what the line is strung with. Its MaxTempC is the limit the
	// rating is the current at, and its resistance curve is evaluated there.
	Conductor conductor.Conductor

	// AmbientK is air temperature, in kelvin.
	AmbientK float64

	// WindMS is the wind speed at the conductor, in m/s.
	//
	// A mean rather than a gust, which is the opposite of what the mechanical
	// models take. Cooling integrates over minutes — a conductor's thermal time
	// constant is ten to twenty of them — so a three-second gust does not cool
	// it, while the same gust breaks it instantly if it is going to.
	WindMS float64

	// WindAngleDeg is the angle between the wind and the conductor, in degrees.
	// Ninety is a wind straight across, which cools best.
	WindAngleDeg float64

	// CloudCoverPercent is total cloud, 0 to 100. It attenuates solar heating.
	CloudCoverPercent float64

	// LatitudeDeg and LongitudeDeg locate the span, for the sun's position.
	LatitudeDeg, LongitudeDeg float64

	// At is when the rating is for. The sun's position depends on it, and
	// nothing else here does.
	At time.Time

	// ElevationM is height above sea level, in meters. Thin air both cools
	// less and lets more sun through.
	ElevationM float64
}

// Rating is the ampacity and the heat balance it came out of.
type Rating struct {
	// AmpacityA is the current that holds the conductor exactly at its limit,
	// in amperes.
	//
	// Zero when the weather alone already puts it there — see [Ampacity].
	AmpacityA float64

	// ConvectiveWPerM, RadiativeWPerM and SolarWPerM are the terms of the
	// balance, in watts per meter.
	//
	// Reported rather than discarded because a rating on its own is unfalsifiable
	// by eye, while its four terms can each be checked against intuition: solar
	// should be small, convection should dominate in wind, radiation should
	// barely move.
	ConvectiveWPerM, RadiativeWPerM, SolarWPerM float64
}

// Ampacity computes the steady-state thermal rating, after IEEE Std 738.
//
// The balance, evaluated with the conductor at its temperature limit:
//
//	q_c + q_r = q_s + I²·R(T_max)
//	I = √((q_c + q_r − q_s) / R(T_max))
//
// Convection takes the largest of three correlations — two forced, one natural
// — as the standard specifies. Taking the smallest, or taking a forced
// correlation outside its range, would overstate cooling and therefore the
// rating, which is the direction that lets a line melt.
//
// Solar heating assumes the sun is square to the conductor. The standard
// resolves the sun's azimuth against the line's bearing, but blackstart's spans
// each have their own bearing while a rating is per branch, and the correction
// is worth at most a few percent of a term that is itself worth about fifteen.
// Assuming the worst leaves the rating conservative.
//
// A rating of zero means the weather alone holds the conductor at its limit: the
// ambient is at or above it, or the sun is putting in more heat than convection
// and radiation take out. That is a real answer rather than an error, and it is
// the answer a hazard model exists to produce.
//
// # Errors
//
// Returns an error wrapping bserr.ErrInvalid when the conductor does not
// validate, the ambient is not above absolute zero, the wind speed is negative,
// the cloud cover is outside 0 to 100, or the latitude is outside ±90.
func Ampacity(in RatingInput) (Rating, error) {
	if err := in.Conductor.Validate(); err != nil {
		return Rating{}, err
	}

	if in.AmbientK <= 0 {
		return Rating{}, fmt.Errorf(
			"%w: ambient %v K", bserr.ErrInvalid, in.AmbientK)
	}

	if in.WindMS < 0 {
		return Rating{}, fmt.Errorf(
			"%w: wind %v m/s; convection takes a speed", bserr.ErrInvalid, in.WindMS)
	}

	if in.CloudCoverPercent < 0 || in.CloudCoverPercent > percent {
		return Rating{}, fmt.Errorf(
			"%w: cloud cover %v%%", bserr.ErrInvalid, in.CloudCoverPercent)
	}

	if math.Abs(in.LatitudeDeg) > quarterTurn {
		return Rating{}, fmt.Errorf(
			"%w: latitude %v degrees", bserr.ErrInvalid, in.LatitudeDeg)
	}

	ambientC := in.AmbientK - zeroCelsiusK
	maxC := in.Conductor.MaxTempC

	solar := in.Conductor.Absorptivity *
		SolarFlux(in.LatitudeDeg, in.LongitudeDeg, in.At, in.CloudCoverPercent) *
		solarElevationFactor(in.ElevationM) *
		in.Conductor.DiameterM

	if ambientC >= maxC {
		return Rating{SolarWPerM: solar}, nil
	}

	convective := convection(in, ambientC, maxC)
	radiative := radiation(in.Conductor, ambientC, maxC)

	net := convective + radiative - solar
	rating := Rating{
		ConvectiveWPerM: convective,
		RadiativeWPerM:  radiative,
		SolarWPerM:      solar,
	}

	if net <= 0 {
		return rating, nil
	}

	rating.AmpacityA = math.Sqrt(net / in.Conductor.ResistanceAt(maxC))

	return rating, nil
}

// convection is the heat a meter of conductor loses to the air, in W/m.
//
// Air properties are evaluated at the film temperature — the mean of the
// conductor and the air — because that is the temperature of the air actually
// doing the carrying. Evaluating them at ambient is a common shortcut and
// overstates the rating.
func convection(in RatingInput, ambientC, maxC float64) float64 {
	filmC := (maxC + ambientC) / 2
	filmK := filmC + zeroCelsiusK

	viscosity := viscosityCoefficient * math.Pow(filmK, 1.5) / (filmK + sutherlandK)

	density := (densityAtSeaLevel +
		densityPerMeter*in.ElevationM +
		densityPerMeterSq*in.ElevationM*in.ElevationM) /
		(1 + densityThermalPerDegC*filmC)

	conductivity := conductivityA + conductivityB*filmC + conductivityC*filmC*filmC

	delta := maxC - ambientC
	reynolds := in.Conductor.DiameterM * density * in.WindMS / viscosity
	angle := windDirectionFactor(in.WindAngleDeg)

	forcedLow := (lowWindOffset + lowWindFactor*math.Pow(reynolds, lowWindExponent)) *
		conductivity * delta * angle
	forcedHigh := highWindFactor * math.Pow(reynolds, highWindExponent) *
		conductivity * delta * angle

	natural := naturalFactor *
		math.Pow(density, naturalDensityExponent) *
		math.Pow(in.Conductor.DiameterM, naturalDiameterExponent) *
		math.Pow(delta, naturalDeltaExponent)

	return math.Max(natural, math.Max(forcedLow, forcedHigh))
}

// windDirectionFactor is how much of the crosswind's cooling a wind at this
// angle to the conductor delivers.
//
// The angle folds into 0 to 90 degrees first. A conductor does not know which
// end of itself the wind came from, and neither does this correlation: it is
// fitted over a quarter turn and runs away outside it.
func windDirectionFactor(deg float64) float64 {
	folded := math.Abs(math.Mod(deg, halfTurnDeg))
	if folded > quarterTurn {
		folded = halfTurnDeg - folded
	}

	rad := radians(folded)

	return angleOffset - math.Cos(rad) + angleCosTwo*math.Cos(2*rad) +
		angleSinTwo*math.Sin(2*rad)
}

// radiation is the heat a meter of conductor radiates away, in W/m.
func radiation(c conductor.Conductor, ambientC, maxC float64) float64 {
	hot := (maxC + radiationK) / radiationDiv
	cold := (ambientC + radiationK) / radiationDiv

	return radiationFactor * c.DiameterM * c.Emissivity *
		(hot*hot*hot*hot - cold*cold*cold*cold)
}

// AmpacityUnit is what [Rating.AmpacityA] is in, for a caller assembling a
// typed quantity.
func AmpacityUnit() quantity.Unit { return quantity.Ampere }
