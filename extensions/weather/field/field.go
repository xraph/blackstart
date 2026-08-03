// Package field is the closed vocabulary of weather quantities this platform
// consumes.
//
// Closed for the same reason source.Kind is closed: an open vocabulary lets a
// misspelling become a silently empty series. "gust_10m" would store,
// read back as nothing, and produce a hazard calculation conditioned on a
// field that was never populated — which is indistinguishable from a calm.
//
// Each field carries two things. Its unit, as a quantity.Unit rather than a
// string, so that a hazard model cannot add a gust to an ambient temperature —
// IEEE 738 takes both. And its GRIB2 parameter identity, so that one field
// name resolves against both HRRR and GFS, whose parameter numbering agrees
// but whose level conventions do not always.
package field

import (
	"fmt"
	"slices"

	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/quantity"
)

// Field names one weather quantity.
type Field string

// The vocabulary, from the Layer B requirement list in the project spec.
const (
	// Gust10m is the peak wind at 10 m. What a conductor and its structures
	// are loaded by; the sustained wind matters less because the failure is
	// driven by the peak.
	Gust10m Field = "gust10m"

	// Wind10m is the sustained wind at 10 m, the standard observing height.
	Wind10m Field = "wind10m"

	// Wind80m is the wind at 80 m — closer to conductor height on a
	// transmission structure than the 10 m standard is.
	Wind80m Field = "wind80m"

	// Temp2m is the ambient temperature, an IEEE 738 input.
	Temp2m Field = "temp2m"

	// Dewpoint2m is the dewpoint, which with temperature gives the icing
	// regime.
	Dewpoint2m Field = "dewpoint2m"

	// PrecipRate is the precipitation mass flux.
	PrecipRate Field = "precip_rate"

	// PrecipType is which phase is falling. Categorical, not a measurement.
	PrecipType Field = "precip_type"

	// FreezingRain is accumulated freezing rain — the direct driver of the
	// ice load an ice-accretion model computes.
	FreezingRain Field = "freezing_rain"

	// Snow is snow depth.
	Snow Field = "snow"

	// RelHumidity is relative humidity.
	RelHumidity Field = "rel_humidity"

	// CloudCover is total cloud cover, which moderates the solar term in a
	// thermal rating.
	CloudCover Field = "cloud_cover"

	// Lightning is the lightning threat index.
	Lightning Field = "lightning"

	// CAPE is convective available potential energy — the storm-severity
	// proxy.
	CAPE Field = "cape"
)

// vocabulary is every field, in a fixed order.
//
//nolint:gochecknoglobals // a fixed vocabulary, not mutable state
var vocabulary = []Field{
	Gust10m, Wind10m, Wind80m, Temp2m, Dewpoint2m,
	PrecipRate, PrecipType, FreezingRain, Snow,
	RelHumidity, CloudCover, Lightning, CAPE,
}

// Fields returns every field in the vocabulary, in a stable order.
//
// Stable because it drives the ingest field list, and a set iterated in map
// order would fetch byte ranges in a different sequence every run — which
// makes a partial ingest impossible to compare against the one before it.
//
// A fresh slice each call, because callers hold the result and a shared
// backing array would let one of them reorder the vocabulary for everybody.
func Fields() []Field { return slices.Clone(vocabulary) }

// Valid reports whether this is a field the vocabulary knows.
func (f Field) Valid() bool { return slices.Contains(vocabulary, f) }

// String renders the field name.
func (f Field) String() string { return string(f) }

// Spec is how to find a field in a GRIB2 file, and what its numbers mean.
type Spec struct {
	// Field is the name this spec describes.
	Field Field

	// Unit is what the source reports in.
	//
	// Not converted at ingest: a stored number that disagrees with the bytes
	// its provenance chain leads to is not much of a chain, and which
	// conversion a reader wants depends on what they are doing.
	Unit quantity.Unit

	// Discipline, Category and Number are the WMO code table 4.2 coordinates
	// that name a parameter.
	Discipline uint8
	Category   uint8
	Number     uint8

	// LevelType is WMO code table 4.5 — 103 is "specified height above
	// ground", 1 is "ground or water surface", 10 is "entire atmosphere".
	LevelType uint8

	// LevelValue is the height in meters when LevelType is 103, and zero
	// otherwise.
	//
	// It is the only coordinate distinguishing 10 m wind from 80 m wind, which
	// agree on every other one. Dropping it would make one of the two
	// unreachable.
	LevelValue float64
}

// Level types from WMO code table 4.5, named so the table below reads.
const (
	levelSurface     uint8 = 1
	levelEntireAtmos uint8 = 10
	levelHeightAbove uint8 = 103
)

// specs maps the vocabulary onto GRIB2.
//
// The coordinates are from WMO code tables 4.2 and 4.5. They are verified
// against real HRRR and GFS files by the decoder work, not here — a number
// that is uniquely wrong decodes the wrong message and no test in this package
// can see that. What this package's tests do catch is two fields colliding on
// one identity, which would leave one of them never populated.
//
//nolint:gochecknoglobals // a fixed table, not mutable state
var specs = map[Field]Spec{
	Gust10m: {
		Field: Gust10m, Unit: quantity.MeterPerSecond,
		Discipline: 0, Category: 2, Number: 22,
		LevelType: levelHeightAbove, LevelValue: 10,
	},
	Wind10m: {
		Field: Wind10m, Unit: quantity.MeterPerSecond,
		Discipline: 0, Category: 2, Number: 1,
		LevelType: levelHeightAbove, LevelValue: 10,
	},
	Wind80m: {
		Field: Wind80m, Unit: quantity.MeterPerSecond,
		Discipline: 0, Category: 2, Number: 1,
		LevelType: levelHeightAbove, LevelValue: 80,
	},
	Temp2m: {
		Field: Temp2m, Unit: quantity.Kelvin,
		Discipline: 0, Category: 0, Number: 0,
		LevelType: levelHeightAbove, LevelValue: 2,
	},
	Dewpoint2m: {
		Field: Dewpoint2m, Unit: quantity.Kelvin,
		Discipline: 0, Category: 0, Number: 6,
		LevelType: levelHeightAbove, LevelValue: 2,
	},
	PrecipRate: {
		Field: PrecipRate, Unit: quantity.KilogramPerSquareMeterSecond,
		Discipline: 0, Category: 1, Number: 7,
		LevelType: levelSurface,
	},
	// Categorical: a code table value, not a measurement. Ratio is the honest
	// unit for "this number is an index" — giving it a physical dimension
	// would let it be added to something.
	PrecipType: {
		Field: PrecipType, Unit: quantity.Ratio,
		Discipline: 0, Category: 1, Number: 19,
		LevelType: levelSurface,
	},
	FreezingRain: {
		Field: FreezingRain, Unit: quantity.KilogramPerSquareMeter,
		Discipline: 0, Category: 1, Number: 227,
		LevelType: levelSurface,
	},
	Snow: {
		Field: Snow, Unit: quantity.Millimeter,
		Discipline: 0, Category: 1, Number: 11,
		LevelType: levelSurface,
	},
	RelHumidity: {
		Field: RelHumidity, Unit: quantity.Percent,
		Discipline: 0, Category: 1, Number: 1,
		LevelType: levelHeightAbove, LevelValue: 2,
	},
	CloudCover: {
		Field: CloudCover, Unit: quantity.Percent,
		Discipline: 0, Category: 6, Number: 1,
		LevelType: levelEntireAtmos,
	},
	// An index rather than a flash density, which is why it is a ratio.
	Lightning: {
		Field: Lightning, Unit: quantity.Ratio,
		Discipline: 0, Category: 17, Number: 192,
		LevelType: levelSurface,
	},
	CAPE: {
		Field: CAPE, Unit: quantity.JoulePerKilogram,
		Discipline: 0, Category: 7, Number: 6,
		LevelType: levelSurface,
	},
}

// SpecFor returns how to find a field in a GRIB2 file.
//
// # Errors
//
// Returns an error wrapping bserr.ErrInvalid when the field is not in the
// vocabulary.
func SpecFor(f Field) (Spec, error) {
	spec, ok := specs[f]
	if !ok {
		return Spec{}, fmt.Errorf(
			"%w: %q is not a weather field this platform knows; the vocabulary "+
				"is closed so that a misspelling cannot become a silently empty "+
				"series", bserr.ErrInvalid, f)
	}

	return spec, nil
}
