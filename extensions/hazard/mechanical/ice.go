package mechanical

import (
	"fmt"
	"math"

	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/quantity"
)

// Constants of the accretion model.
const (
	// iceDensityKgPerM3 is glaze ice — the clear, dense ice freezing rain
	// leaves. Rime is half this or less, but rime forms in cloud rather than in
	// freezing rain, and freezing rain is what this model is for.
	iceDensityKgPerM3 = 917

	// freezingK is where accretion stops.
	freezingK = 273.15

	// gravityMPerS2 is standard gravity, turning the ice's mass into the weight
	// hanging off the conductor.
	gravityMPerS2 = 9.80665

	// Best's relation between rainfall rate and liquid water content:
	//
	//	W [g/m3] = 0.067 · P [mm/h] ^ 0.846
	//
	// It supplies the one quantity Jones's collection term needs that no
	// forecast product publishes. An empirical fit, and the weakest link in
	// this model — but the alternative is dropping the wind term entirely,
	// which understates a wind-driven ice storm by a factor of several.
	bestCoefficient = 0.067
	bestExponent    = 0.846

	secondsPerHour   = 3600
	gramsPerKilogram = 1000
)

// IceInput is one weather sample and the conductor it is falling on.
type IceInput struct {
	// FreezingRainKgPerM2 is the liquid accumulated over the accretion period,
	// in kg/m2 — which for water is also millimeters of depth.
	//
	// All of it is assumed to freeze. Freezing rain by definition arrives
	// supercooled at a surface below freezing, and the shedding and runoff that
	// would reduce the yield are not something a forecast field can tell us
	// about.
	FreezingRainKgPerM2 float64

	// PrecipRateKgPerM2S is the instantaneous precipitation rate, in kg/m2/s.
	//
	// Used only for the collection term, which needs the ratio of horizontal to
	// vertical flux rather than either alone. Zero means no wind enhancement is
	// applied, which is the neutral reading of a sample that reports
	// accumulation without a rate.
	PrecipRateKgPerM2S float64

	// TempK is air temperature, in kelvin. At or below freezing, ice accretes;
	// above it, nothing does.
	TempK float64

	// WindMS is the wind speed driving droplets onto the conductor's side, in
	// m/s. Speed only: accretion wraps the cylinder, so which way the wind
	// blows changes where the ice sits but not how much of it there is.
	WindMS float64

	// DiameterM is the bare conductor diameter, in meters — the width of the
	// target the falling rain has to hit.
	DiameterM float64
}

// IceLoad is the sleeve of ice on a meter of that conductor.
type IceLoad struct {
	// ThicknessM is the equivalent radial thickness, in meters: how thick the
	// accreted mass would be if it were a uniform sleeve.
	//
	// Real accretion is lopsided — thicker on the windward face, sometimes
	// drooping into a fin as the conductor rolls under the load. Equivalent
	// radial thickness is the standard way to state it anyway, because it is
	// the quantity that determines weight and drag, and because no model
	// predicts the shape.
	ThicknessM float64

	// MassKgPerM is the ice on one meter of conductor. Consistent with
	// ThicknessM by construction — see [Ice].
	MassKgPerM float64
}

// CombinedLoad is the wind and the ice acting together on a meter of
// conductor.
type CombinedLoad struct {
	// HorizontalNPerM is the wind's drag, in newtons per meter.
	HorizontalNPerM float64

	// VerticalNPerM is the ice's weight, in newtons per meter.
	//
	// The ice only. The conductor's own weight is not included, because the
	// conductor library does not carry it and because this quantity exists to
	// describe what the weather added, not what was always there.
	VerticalNPerM float64

	// MagnitudeNPerM is the resultant, in newtons per meter. What the
	// attachment actually pulls against.
	MagnitudeNPerM float64

	// AngleDeg is the resultant's swing from straight down, 0 to 90 degrees.
	// Zero is ice hanging in still air, ninety is wind on a bare conductor.
	AngleDeg float64
}

// Ice computes the ice a freezing-rain event leaves on a conductor, after
// Jones (1998), "A simple model for freezing rain ice loads", Atmospheric
// Research 46, 87-97. The 1996 CRREL report of the same author is a different
// document; this is the journal model.
//
// The collected mass is the flux onto the conductor's projected width. Rain
// falls onto the top, and wind drives droplets onto the side; the two fluxes
// are perpendicular, so they combine as a vector rather than a sum:
//
//	m/L = D · A · √(1 + (W·V / P)²)
//
// where A is the liquid accumulation, W the liquid water content, V the wind
// speed and P the precipitation rate. The ratio W·V/P is the horizontal flux
// over the vertical one, and using the ratio rather than the two fluxes
// separately is what lets this work from an accumulation without needing to
// know how long the event lasted.
//
// That mass is then reported as the sleeve it would form:
//
//	m/L = ρ_ice · π · t · (D + t)
//
// solved for t. The returned mass is recomputed from the returned thickness
// rather than carried alongside it, so the two cannot drift apart.
//
// Above freezing the result is zero regardless of precipitation. That check is
// the one that keeps a warm rainstorm from derating a line.
//
// # Errors
//
// Returns an error wrapping bserr.ErrInvalid when the diameter is not positive,
// the temperature is not above absolute zero, or any of the accumulation, rate
// or wind speed is negative.
func Ice(in IceInput) (IceLoad, error) {
	if in.DiameterM <= 0 {
		return IceLoad{}, fmt.Errorf(
			"%w: conductor diameter %v m; nothing that wide catches rain",
			bserr.ErrInvalid, in.DiameterM)
	}

	if in.TempK <= 0 {
		return IceLoad{}, fmt.Errorf(
			"%w: air temperature %v K", bserr.ErrInvalid, in.TempK)
	}

	if in.FreezingRainKgPerM2 < 0 || in.PrecipRateKgPerM2S < 0 {
		return IceLoad{}, fmt.Errorf(
			"%w: freezing rain %v kg/m2 at %v kg/m2/s; precipitation does not "+
				"run backwards", bserr.ErrInvalid,
			in.FreezingRainKgPerM2, in.PrecipRateKgPerM2S)
	}

	if in.WindMS < 0 {
		return IceLoad{}, fmt.Errorf(
			"%w: wind %v m/s; accretion takes a speed, not a component",
			bserr.ErrInvalid, in.WindMS)
	}

	if in.TempK > freezingK || in.FreezingRainKgPerM2 == 0 {
		return IceLoad{}, nil
	}

	mass := in.DiameterM * in.FreezingRainKgPerM2 * collection(in)

	// m = ρπt(D+t) is a quadratic in t. Only the positive root is a sleeve.
	sleeve := mass / (iceDensityKgPerM3 * math.Pi)
	thickness := (math.Sqrt(in.DiameterM*in.DiameterM+4*sleeve) - in.DiameterM) / 2

	return IceLoad{
		ThicknessM: thickness,
		MassKgPerM: iceDensityKgPerM3 * math.Pi * thickness * (in.DiameterM + thickness),
	}, nil
}

// collection is the factor by which wind-driven droplets raise the yield over
// what falls vertically: √(1 + (W·V/P)²), one when there is no wind or no rate
// to compare against.
func collection(in IceInput) float64 {
	if in.PrecipRateKgPerM2S == 0 || in.WindMS == 0 {
		return 1
	}

	// Best's relation wants millimeters per hour and returns grams per cubic
	// meter; both ends are converted here so the ratio below is in SI. A
	// kilogram of water per square meter is a millimeter deep, which is what
	// makes the first conversion only a matter of seconds to hours.
	rateMMPerHour := in.PrecipRateKgPerM2S * secondsPerHour
	waterKgPerM3 := bestCoefficient * math.Pow(rateMMPerHour, bestExponent) / gramsPerKilogram

	ratio := waterKgPerM3 * in.WindMS / in.PrecipRateKgPerM2S

	return math.Sqrt(1 + ratio*ratio)
}

// Combined resolves the wind's sideways push and the ice's downward weight into
// the single force the conductor's attachments actually see.
//
// The two are perpendicular by construction — drag acts across the span in the
// horizontal plane, weight acts down — so the resultant is the hypotenuse and
// the angle is the swing off vertical.
//
// It cannot fail: both inputs come from models that already validated theirs.
func Combined(wind WindLoad, ice IceLoad) CombinedLoad {
	horizontal := wind.DragNPerM
	vertical := ice.MassKgPerM * gravityMPerS2

	angle := 0.0
	if horizontal != 0 || vertical != 0 {
		angle = math.Atan2(horizontal, vertical) * degreesPerHalfTurn / math.Pi
	}

	return CombinedLoad{
		HorizontalNPerM: horizontal,
		VerticalNPerM:   vertical,
		MagnitudeNPerM:  math.Hypot(horizontal, vertical),
		AngleDeg:        angle,
	}
}

// IceMassUnit is what [IceLoad.MassKgPerM] is in, for a caller assembling a
// typed quantity.
func IceMassUnit() quantity.Unit { return quantity.KilogramPerMeter }
