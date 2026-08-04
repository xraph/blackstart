// Package mechanical is what the weather does to a conductor as a structure:
// the sideways push of wind, the weight of accreted ice, and the two together.
//
// Everything here is a pure function of a weather sample and a conductor. No
// storage, no context, no clock — which is what makes the models testable
// against published worked examples rather than only against themselves.
//
// Loads are per meter of conductor, because that is the form transmission
// engineering states them in and the form that composes: a span's total load is
// its length times the load per meter, and nothing here needs to know the
// length to compute the rest.
package mechanical

import (
	"fmt"
	"math"

	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/quantity"
)

// Constants of the wind model.
const (
	// dragCoefficient for a stranded conductor at the Reynolds numbers
	// transmission wind produces. Round, and rough enough that the boundary
	// layer stays attached the way a smooth cylinder's does not.
	//
	// Held here rather than on [Conductor] because it is a property of the
	// flow regime, not of the conductor: every conductor in the library is a
	// stranded cylinder and takes the same figure.
	dragCoefficient = 1.0

	// airDensityAtZeroC is dry air at 0 C and one standard atmosphere, in
	// kg/m3. Scaled to the sample's temperature by the ideal gas law.
	//
	// Pressure is not corrected for. It varies by a few percent across weather
	// and a good deal more with elevation, and correcting for one and not the
	// other would be a precision this model does not have.
	airDensityAtZeroC = 1.293

	// zeroCelsiusK anchors that density.
	zeroCelsiusK = 273.15

	degreesPerHalfTurn = 180
)

// WindInput is one weather sample and the conductor it is blowing on.
type WindInput struct {
	// GustMS is the wind speed to load with, in m/s. A gust rather than a mean
	// because drag goes as the square of speed, so the mean of the load is not
	// the load at the mean, and a conductor fails on the peak.
	GustMS float64

	// WindUMS and WindVMS are the eastward and northward components of the
	// mean wind, in m/s. They supply direction only; the magnitude comes from
	// GustMS.
	//
	// Direction from the mean and speed from the gust is the standard
	// simplification: a gust is a fluctuation of the prevailing flow, and no
	// forecast product publishes a gust direction.
	WindUMS, WindVMS float64

	// TempK is air temperature, in kelvin. It sets the air's density.
	TempK float64

	// SpanAzimuthDeg is the span's bearing, degrees clockwise from true north.
	// Treated as a line rather than an arrow — see [Wind].
	SpanAzimuthDeg float64

	// DiameterM is the bare conductor diameter, in meters.
	DiameterM float64

	// IceThicknessM is the radial thickness of accreted ice, in meters. Zero
	// for a bare conductor.
	//
	// It widens the face the wind pushes on by twice itself, which is why the
	// accretion model runs before this one.
	IceThicknessM float64
}

// WindLoad is what the wind does to a meter of that conductor.
type WindLoad struct {
	// DragNPerM is the transverse force per meter of conductor, in newtons per
	// meter.
	DragNPerM float64

	// CrosswindMS is the component of the gust across the span, in m/s. The
	// only part of the wind that loads it.
	CrosswindMS float64

	// AngleDeg is the angle between the wind and the span, 0 to 90 degrees.
	// Ninety is a wind straight across the span, zero one straight along it.
	//
	// Recorded because it is the number an engineer looking at a surprising
	// drag figure checks first.
	AngleDeg float64
}

// Wind computes the transverse load a gust puts on a conductor.
//
//	θ_wind = atan2(-u, -v)                 the direction the wind blows FROM
//	V⊥     = gust · |sin(θ_wind − θ_span)|
//	ρ_air  = 1.293 · 273.15 / T
//	F/L    = ½ · ρ_air · C_d · (D + 2·t_ice) · V⊥²
//
// The atan2 arguments are the error-prone part and are negated deliberately.
// The components u and v say where the air is going; meteorological wind
// direction names where it comes from, and the whole vocabulary this platform
// consumes — "a northerly", the direction on every forecast product — uses the
// latter. Negating both reverses the vector, and passing u before v converts
// from the mathematical convention (counterclockwise from east) to the compass
// one (clockwise from north).
//
// Only the absolute sine matters, so both the span's bearing and the wind's
// direction are effectively taken modulo half a turn. A span surveyed
// north-to-south loads the same as the same span surveyed south-to-north, and a
// westerly loads a north-south span the same as an easterly.
//
// When u and v are both zero the direction is undefined, and the gust is taken
// as fully across the span. That is the conservative reading of a sample that
// reports a gust with no prevailing flow, and it is a sample worth being
// conservative about.
//
// # Errors
//
// Returns an error wrapping bserr.ErrInvalid when the diameter is not positive,
// the gust or ice thickness is negative, or the temperature is not above
// absolute zero.
func Wind(in WindInput) (WindLoad, error) {
	if in.DiameterM <= 0 {
		return WindLoad{}, fmt.Errorf(
			"%w: conductor diameter %v m; the drag face cannot be empty",
			bserr.ErrInvalid, in.DiameterM)
	}

	if in.GustMS < 0 {
		return WindLoad{}, fmt.Errorf(
			"%w: gust %v m/s; speed is a magnitude and direction comes from u and v",
			bserr.ErrInvalid, in.GustMS)
	}

	if in.IceThicknessM < 0 {
		return WindLoad{}, fmt.Errorf(
			"%w: ice thickness %v m", bserr.ErrInvalid, in.IceThicknessM)
	}

	if in.TempK <= 0 {
		return WindLoad{}, fmt.Errorf(
			"%w: air temperature %v K; density goes to infinity at absolute zero",
			bserr.ErrInvalid, in.TempK)
	}

	angle := degreesPerHalfTurn / 2.0

	if in.WindUMS != 0 || in.WindVMS != 0 {
		from := math.Atan2(-in.WindUMS, -in.WindVMS) * degreesPerHalfTurn / math.Pi
		angle = incidence(from - in.SpanAzimuthDeg)
	}

	crosswind := in.GustMS * math.Sin(angle*math.Pi/degreesPerHalfTurn)
	density := airDensityAtZeroC * zeroCelsiusK / in.TempK
	face := in.DiameterM + 2*in.IceThicknessM

	return WindLoad{
		DragNPerM:   0.5 * density * dragCoefficient * face * crosswind * crosswind,
		CrosswindMS: crosswind,
		AngleDeg:    angle,
	}, nil
}

// incidence folds an angle between two bearings into 0 to 90 degrees.
//
// Both bearings name lines rather than arrows, so a difference of 170 degrees
// is a 10-degree incidence and a difference of 190 is the same 10.
func incidence(deg float64) float64 {
	folded := math.Abs(math.Mod(deg, degreesPerHalfTurn))
	if folded > degreesPerHalfTurn/2 {
		folded = degreesPerHalfTurn - folded
	}

	return folded
}

// DragUnit is what [WindLoad.DragNPerM] is in, for a caller assembling a typed
// quantity.
func DragUnit() quantity.Unit { return quantity.NewtonPerMeter }
