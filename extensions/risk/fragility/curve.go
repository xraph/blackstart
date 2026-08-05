// Package fragility is the mathematics that turns a wind or ice load into a
// probability of failure.
//
// Pure arithmetic: no I/O, no storage, no clock, and no dependency on any
// other extension. That is what makes a fragility curve testable against its
// own properties — bounded, monotone, correct at its own median — rather than
// only against a golden number nobody can independently check.
//
// Every curve in this package is cited in a comment beside its parameters.
// Section 8 of the project spec treats a fragility function as literature
// rather than a feed: a wrong number here is invisible once it is stored and
// multiplied through a forecast, so nothing here is invented. Where the
// literature does not defensibly cover a structure class, that class is
// absent rather than approximated — see [Class].
package fragility

import "math"

// Class is the closed set structure curves are indexed by.
//
// Portal (H-frame) structures are deliberately not a member. The literature
// search conducted for this package included PNNL-33587's fragility function
// catalog, which carries no portal/H-frame entry with a numeric median and
// dispersion (its wind and hurricane sections cover wind turbines, poles,
// circuit breakers, telecom towers, and lattice towers only). Note for a
// reader tempted to look there: the Lattice and Pole curves stored below do
// not come from PNNL-33587 either — they are read directly from the journal
// papers cited beside each in the library below (Liu & Yan 2022; Mohammadi
// Darestani & Shafieezadeh 2019). No open-access or national-laboratory
// source turned up a numeric median and dispersion for Portal specifically.
// A plausible-looking entry would be indistinguishable from a measured one
// once it is stored and multiplied through a forecast — see the package
// doc — so the class is omitted rather than approximated. [ByClass] reports
// false for it because it is not a member of this type, not because of a
// runtime lookup miss.
type Class string

const (
	// Lattice is a self-supporting steel lattice tower.
	Lattice Class = "lattice"

	// Pole is a single wood or steel shaft.
	Pole Class = "pole"
)

// Curve is a lognormal fragility: P(fail | IM) = Φ((ln IM − ln θ)/β).
type Curve struct {
	Median     float64 // θ, in the intensity measure's own unit
	Dispersion float64 // β, the logarithmic standard deviation
	Source     string  // the citation, verbatim
}

// Probability evaluates the curve at an intensity measure.
//
// Zero and negative intensities return zero rather than evaluating: ln(0) is
// negative infinity, and the NaN that follows would propagate silently through
// every county-hour sum downstream instead of failing where it was produced.
//
// A non-positive Dispersion gets the same treatment. Curve is exported and
// constructible by any caller, and a zero Dispersion divides by zero at
// exactly intensity == Median — 0/0, which is NaN, not a clean infinity —
// so it is guarded the same way rather than trusted to never occur.
func (c Curve) Probability(intensity float64) float64 {
	if intensity <= 0 || c.Dispersion <= 0 {
		return 0
	}

	// Φ(x) = ½·erfc(−x/√2); math.Erfc is the standard normal CDF written
	// without a table, which is the whole of what a statistics dependency
	// would buy for this one expression.
	return 0.5 * math.Erfc(-(math.Log(intensity)-math.Log(c.Median))/(c.Dispersion*math.Sqrt2))
}

// Reference conductor used to convert a published median from wind speed to
// wind load, for citations that report their fragility in speed rather than
// in the load per meter this platform's hazard extension publishes.
//
// F/L = ½·ρ_air·C_d·D·V² is the same drag equation
// extensions/hazard/mechanical.Wind uses; this package cannot import that
// extension (it depends on nothing), so the equation is restated here rather
// than shared.
const (
	// referenceConductorDiameterM is ACSR 795 kcmil 26/7 Drake, 28.14 mm —
	// the same reference conductor extensions/hazard/conductor's library
	// keys off IEEE 738's worked example, chosen here for the same reason:
	// it is the conductor this codebase already treats as its standard
	// worked example, not a value picked to make a conversion come out a
	// particular way.
	referenceConductorDiameterM = 0.02814

	// referenceAirDensityKgPerM3 is ISO 2533 standard atmosphere at sea
	// level, 15 C — the conventional reference density for a wind-load
	// calculation that is not itself tied to a specific weather sample.
	referenceAirDensityKgPerM3 = 1.225

	// referenceDragCoefficient for a round stranded cylinder, matching
	// extensions/hazard/mechanical's dragCoefficient.
	referenceDragCoefficient = 1.0
)

// windLoadFromSpeed converts a wind speed in m/s to the transverse load, in
// N/m, the reference conductor would see at that speed.
//
// What this indexes the curve on and what compute.go actually feeds it are
// not the same quantity, and that gap is not closed anywhere in this
// package. This function's V is the full, undirected gust speed against a
// single fixed reference diameter (referenceConductorDiameterM, Drake's
// 0.02814 m). What Curve.Probability is evaluated against in compute.go is
// hazard's wind_load, computed by extensions/hazard/mechanical.Wind as
// ½·ρ(T)·Cd·(D_span + 2·t_ice)·V⊥², where V⊥ = gust × |sin(angle between
// wind and span azimuth)| — a value that depends on the span's bearing
// relative to the wind and on the real conductor's diameter, neither of
// which this function knows about.
//
// Two consequences follow, and with the Lattice curve's β = 0.0794 giving it
// a near-step transition at 15.85 N/m, both dominate the answer rather than
// perturbing it:
//
//   - Azimuth: a 40 m/s gust 20 degrees off the span's line gives
//     V⊥ ≈ 13.7 m/s and a load around 3.2 N/m (P ≈ 0); the same gust
//     perpendicular to the span gives V⊥ = 40 m/s and a load around
//     27.6 N/m (P ≈ 1). One tower's collapse probability is not a function
//     of which way its line happens to run, but as fed here, it is.
//   - Conductor diameter: the reference diameter is Drake's fixed 0.02814 m;
//     the library in extensions/hazard/conductor spans 0.02179 m (Hawk) to
//     0.03038 m (Cardinal), about 2.6β of swing on the Lattice curve by
//     itself. A structure's fragility should not depend on which conductor
//     library entry happens to be strung on the span next to it, but as fed
//     here, it does.
//
// This is a real modelling limitation, not a bug to fix by re-deriving the
// curve: see docs/extensions/risk.md's Known gaps for the disclosure, and
// the design note at the top of this file for why an invented correction
// would be worse than a stated gap.
func windLoadFromSpeed(speedMS float64) float64 {
	return 0.5 * referenceAirDensityKgPerM3 * referenceDragCoefficient *
		referenceConductorDiameterM * speedMS * speedMS
}

// library holds a fragility curve for a class only when a national-
// laboratory or open-access source supplies a numeric median and dispersion;
// see the [Class] doc for the class considered and rejected for want of one.
var library = map[Class]Curve{
	Lattice: {
		// Liu, C., Yan, Z. (2022). "Fragility Analysis of Wind-Induced
		// Collapse of a Transmission Tower Considering Corrosion." Buildings
		// 12(10), 1500. https://doi.org/10.3390/buildings12101500
		// (open access, CC BY).
		//
		// Section 3.4: for the standard-value model of a 220 kV, 30 m
		// latticed transmission tower, the log-mean and log-standard
		// deviation of the basic collapse wind speed from stripe analysis
		// were 3.412 and 0.0397 respectively — median = e^3.412 ≈ 30.3 m/s.
		// The paper reports the wind speed at the tower; converted here to
		// the load a reference conductor sees at that speed via
		// windLoadFromSpeed, because this platform's intensity measure is
		// wind_load (N/m), not wind speed. 30.3 m/s → 15.85 N/m.
		//
		// windLoadFromSpeed is quadratic (W = kV²), a power-law map, not a
		// linear rescale. A lognormal random variable pushed through a
		// power-law map of exponent p is still lognormal, with its log-scale
		// parameter multiplied by p: V ~ lognormal(ln θ, β) implies
		// W = kV² ~ lognormal(ln θ_W, 2β). The median (θ) was converted
		// through windLoadFromSpeed above; the dispersion (β) must be scaled
		// by that same exponent, 2, or the stored curve is twice as steep in
		// log-load space as the cited paper supports. 0.0397 (the paper's own
		// value, in wind-speed space) × 2 = 0.0794, stored below.
		Median:     windLoadFromSpeed(30.326),
		Dispersion: 0.0794,
		Source: "Liu, C., Yan, Z. (2022). Fragility Analysis of Wind-Induced " +
			"Collapse of a Transmission Tower Considering Corrosion. " +
			"Buildings 12(10), 1500. https://doi.org/10.3390/buildings12101500 " +
			"(sec. 3.4: log-mean 3.412, log-std 0.0397 for basic collapse wind " +
			"speed, m/s; median converted here to wind load via a reference " +
			"conductor, dispersion doubled to 0.0794 to match — the paper's " +
			"0.0397 describes wind speed, not the load stored in Median)",
	},
	Pole: {
		// Mohammadi Darestani, Y., Shafieezadeh, A. (2019). "Multi-
		// dimensional wind fragility functions for wood utility poles."
		// Engineering Structures 183, 937-948.
		// https://doi.org/10.1016/j.engstruct.2018.12.041 (open-access
		// author manuscript via the NSF Public Access Repository,
		// https://par.nsf.gov/servlets/purl/10098529).
		//
		// A Class 3 Southern Yellow Pine pole at 50 years, wind
		// perpendicular to the conductors, 2 m² of conductor area — the
		// same design point the paper itself uses for its Fig. 4b worked
		// example. The paper states the median directly in that discussion:
		// 176 mph (78.68 m/s). Dispersion is not stated for that figure, so
		// it is computed here from the paper's own response-surface
		// equation (22) and Table 16 GLM coefficients at the same design
		// point (θ=90°, Ac=2 m², t=50): σ ≈ 0.2223. Re-deriving the median
		// from the same table gives ≈176.6 mph, confirming the coefficients
		// were read correctly. Converted from 78.68 m/s to 106.7 N/m via
		// windLoadFromSpeed, for the same reason as Lattice above.
		//
		// windLoadFromSpeed is quadratic (see the Lattice entry above for the
		// full derivation), so the dispersion computed above — 0.2223, in
		// wind-speed log-space — must likewise be doubled to describe the
		// load stored in Median: 0.2223 × 2 = 0.4446, stored below.
		Median:     windLoadFromSpeed(78.679),
		Dispersion: 0.4446,
		Source: "Mohammadi Darestani, Y., Shafieezadeh, A. (2019). Multi-" +
			"dimensional wind fragility functions for wood utility poles. " +
			"Engineering Structures 183, 937-948. " +
			"https://doi.org/10.1016/j.engstruct.2018.12.041 (Fig. 4b design " +
			"point: Class 3 pole, age 50 yr, wind perpendicular to " +
			"conductors, 2 m^2 conductor area; median 176 mph stated in " +
			"text, dispersion 0.2223 computed from eq. 22 and Table 16 in " +
			"wind-speed log-space, doubled to 0.4446 to match Median's " +
			"wind-load units; converted here to wind load via a reference " +
			"conductor)",
	},
}

// ByClass returns the wind curve for a structure class.
func ByClass(c Class) (Curve, bool) {
	curve, ok := library[c]

	return curve, ok
}
