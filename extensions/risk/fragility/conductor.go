package fragility

import "math"

// metersPerKm converts a span length in meters to the kilometers
// [ConductorRate] is expressed per.
const metersPerKm = 1000.0

// Constants of the conductor rate model.
//
// Jamieson, Strbac and Bell (2020). Quantification and visualization of
// extreme wind effects on transmission network outage probability and wind
// generation output. IET Smart Grid 3(2), 112-122,
// https://doi.org/10.1049/iet-stg.2019.0145 (open access, CC BY) — derive
// their λ from binned historical fault data (their Fig. 1, reproduced as
// Fig. 21 of PNNL-33587), not from a closed-form equation, and their bins are
// indexed by wind speed in m/s rather than by wind_load (N/m) and ice_load
// (kg/m), the intensity measures this platform's hazard extension publishes.
// There is consequently no published λ(wind_load, ice_load) to reproduce.
// What is reproduced from that paper is its equation (1), the exponential
// aggregation in [ConductorProbability] — the "form" the brief for this
// package asks for.
//
// The constants below build a monotone λ, not a fit: they are chosen so the
// function is zero at zero load, rises smoothly, and saturates at the order
// of magnitude Jamieson's own curves report — on the order of 0.1 faults per
// 100 km-hour for the most exposed 132 kV lines under extreme wind (their
// Fig. 1) — rather than at a value picked to make a test pass.
const (
	// conductorRateCeilingPerKmHour is the rate this function approaches as
	// wind and ice loading grow without bound, in faults per kilometer-hour.
	// 0.1 faults per 100 km-hour, in Jamieson's units, is 0.001 per km-hour.
	conductorRateCeilingPerKmHour = 1e-3

	// conductorWindScaleNPerM is the wind load at which the wind term alone
	// would bring the rate to 1 − 1/e of its ceiling. 100 N/m is the load a
	// Drake conductor (see curve.go's windLoadFromSpeed) sees from roughly a
	// 76.2 m/s gust, not the 54 m/s an earlier version of this comment
	// claimed — windLoadFromSpeed(54) is only 50.3 N/m. 76.2 m/s is not a
	// comfortably mid-range "severe" value with headroom on both sides: it
	// sits almost exactly at the Pole curve's median (78.68 m/s, Mohammadi
	// Darestani & Shafieezadeh 2019) and about 2.5x the Lattice curve's
	// median (30.33 m/s, Liu & Yan 2022). That is deliberate, not a mistake
	// to fix by lowering the scale: this term is where the wind contribution
	// alone reaches 1 − 1/e of the ceiling, not where it reaches the
	// ceiling, and Jamieson et al.'s own reported curves keep rising well
	// past the point structures start failing, so anchoring the half-rise
	// point near the top of this package's structural severity range is
	// consistent with their shape rather than an arbitrary pick.
	conductorWindScaleNPerM = 100.0

	// conductorIceScaleKgPerM is the ice load contributing as much as
	// conductorWindScaleNPerM of wind. Every source this package cites for
	// ice (PNNL-33587 sec. 3.7, Bilionis and Vamvatsikos 2019, telecom
	// towers) treats a few centimeters of accretion as comparable in effect
	// to a large wind-speed increase, not a minor correction — 2 kg/m is
	// on that order for a bare conductor's worth of radial ice.
	conductorIceScaleKgPerM = 2.0
)

// ConductorRate returns λ: expected conductor failures per kilometer per hour
// under the given wind and ice loading.
//
// λ here is a monotone construction calibrated to the right order of
// magnitude, not a fit to Jamieson, Strbac and Bell (2020)'s data: their own
// λ is empirical, taken from binned historical fault-rate tables indexed by
// wind speed, and is not published in closed form for this platform's
// wind_load/ice_load units. See the constants below for the full citation
// and for what is and is not reproduced from that paper.
//
// Negative inputs are clamped to zero rather than allowed to cancel a
// positive term: a negative load is not physically meaningful, and treating
// it as relief from the other term would be a silent sign error.
func ConductorRate(windLoadNPerM, iceLoadKgPerM float64) float64 {
	windTerm := max(windLoadNPerM, 0) / conductorWindScaleNPerM
	iceTerm := max(iceLoadKgPerM, 0) / conductorIceScaleKgPerM

	return conductorRateCeilingPerKmHour * (1 - math.Exp(-(windTerm + iceTerm)))
}

// ConductorProbability is 1 − exp(−λ·L·Δt), the probability that the
// conductor on a span of lengthM fails within hours.
//
// This is equation (1) of Jamieson, Strbac and Bell (2020) — see the package
// constants above — with L in kilometers and Δt in hours, both reproduced
// directly since the equation, unlike λ itself, is open access and exact.
//
// A span that is not there cannot fail, and neither can one observed for no
// time at all: lengthM and hours that are not strictly positive return zero
// rather than a probability computed over a length or duration that does not
// exist.
func ConductorProbability(windLoadNPerM, iceLoadKgPerM, lengthM, hours float64) float64 {
	if lengthM <= 0 || hours <= 0 {
		return 0
	}

	lambda := ConductorRate(windLoadNPerM, iceLoadKgPerM)

	return 1 - math.Exp(-lambda*(lengthM/metersPerKm)*hours)
}
