// Package thermal is how much current a conductor can carry before it gets too
// hot, given the weather standing around it.
//
// The model is IEEE Std 738, the steady-state heat balance: a conductor loses
// heat by convection and radiation, gains it from the sun and from its own
// resistance, and the rating is the current that puts those in balance exactly
// at the conductor's temperature limit.
//
// Everything here is a pure function. The one external reference this platform
// checks itself against — the standard's own worked example — lives in this
// package's tests.
package thermal

import (
	"math"
	"time"
)

// Constants of the solar model.
const (
	// Coefficients of IEEE 738-2012's clear-atmosphere flux polynomial, in
	// W/m2 against solar altitude in degrees.
	//
	// A polynomial rather than the table it was fitted to, because the table
	// is coarse and interpolating it by hand would reproduce the fit badly.
	// Industrial-atmosphere coefficients also exist in the standard; clear air
	// is used here because it gives the larger flux and therefore the smaller
	// rating.
	clearA = -42.2391
	clearB = 63.8044
	clearC = -1.9220
	clearD = 3.46921e-2
	clearE = -3.61118e-4
	clearF = 1.94318e-6
	clearG = -4.07608e-9

	// Elevation correction on the solar flux, from the same standard.
	elevationLinear    = 1.148e-4
	elevationQuadratic = -1.108e-8

	// Kasten and Czeplak (1980): global irradiance under cloud, as a fraction
	// of the clear-sky value, is 1 − 0.75·c^3.4 for cloud fraction c.
	//
	// The standard itself has no cloud term — it rates for clear sky and stops.
	// Blackstart rates against a forecast, and a forecast that says overcast
	// and is treated as clear overstates solar heating by a factor of four.
	cloudDepth    = 0.75
	cloudExponent = 3.4

	// Earth's axial tilt, which sets the declination's swing.
	obliquityDeg = 23.4583

	daysPerYear        = 365
	solsticeOffsetDays = 284
	degreesPerHour     = 15
	solarNoonHour      = 12
	minutesPerHour     = 60
	secondsPerHour     = 3600
	fullTurnDeg        = 360
	halfTurnDeg        = 180
	percent            = 100
)

// SolarFlux is the solar heat flux reaching a surface square to the sun's rays
// at sea level, in W/m2.
//
// Zero when the sun is below the horizon, which is the case this has to get
// right: a rating computed at night with a daytime flux would be low by around
// fifteen percent and look entirely reasonable.
//
// The solar position is the standard's own: declination from the day of the
// year, hour angle from apparent solar time. Apparent solar time here is UTC
// offset by the longitude alone, with no equation of time, which puts the sun
// up to about a quarter of an hour out of place across the year. That moves the
// flux by well under one percent away from sunrise and sunset, and the standard
// makes the same approximation.
//
// Cloud attenuates the result. The standard has no cloud term because it rates
// for a clear sky by convention; this platform rates against a forecast, and a
// forecast that says overcast has to be allowed to say so.
//
// Elevation is not applied here — see [Ampacity], which has it.
func SolarFlux(latDeg, lonDeg float64, at time.Time, cloudPercent float64) float64 {
	altitude := SolarAltitudeDeg(latDeg, lonDeg, at)
	if altitude <= 0 {
		return 0
	}

	flux := clearA + altitude*(clearB+altitude*(clearC+altitude*(clearD+
		altitude*(clearE+altitude*(clearF+altitude*clearG)))))

	// The fit dips below zero near the horizon, where it is extrapolating past
	// the table it came from.
	if flux < 0 {
		return 0
	}

	return flux * cloudFactor(cloudPercent)
}

// SolarAltitudeDeg is the sun's angle above the horizon, in degrees. Negative
// when it has set.
//
// Exported because the sun's position is worth being able to check on its own:
// a rating that comes out wrong is far easier to diagnose when the altitude
// that fed it can be printed.
func SolarAltitudeDeg(latDeg, lonDeg float64, at time.Time) float64 {
	utc := at.UTC()

	// Declination from the day of the year. The 284 puts the zero crossing at
	// the equinox and the sine carries it through the year.
	dayAngle := radians(fullTurnDeg * float64(solsticeOffsetDays+utc.YearDay()) / daysPerYear)
	declination := radians(obliquityDeg) * math.Sin(dayAngle)

	// Apparent solar time: UTC shifted by longitude, fifteen degrees to the
	// hour, east positive.
	hours := float64(utc.Hour()) +
		float64(utc.Minute())/minutesPerHour +
		float64(utc.Second())/secondsPerHour
	solar := hours + lonDeg/degreesPerHour
	hourAngle := radians((solar - solarNoonHour) * degreesPerHour)

	lat := radians(latDeg)

	sine := math.Cos(lat)*math.Cos(declination)*math.Cos(hourAngle) +
		math.Sin(lat)*math.Sin(declination)

	return degrees(math.Asin(math.Max(-1, math.Min(1, sine))))
}

// cloudFactor is the fraction of clear-sky flux that gets through.
func cloudFactor(cloudPercent float64) float64 {
	fraction := math.Max(0, math.Min(1, cloudPercent/percent))

	return 1 - cloudDepth*math.Pow(fraction, cloudExponent)
}

// solarElevationFactor is the thinner air's smaller absorption, as a multiple
// of the sea-level flux.
func solarElevationFactor(elevationM float64) float64 {
	return 1 + elevationLinear*elevationM + elevationQuadratic*elevationM*elevationM
}

func radians(deg float64) float64 { return deg * math.Pi / halfTurnDeg }

func degrees(rad float64) float64 { return rad * halfTurnDeg / math.Pi }
