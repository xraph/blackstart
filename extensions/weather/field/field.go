// Package field is the closed vocabulary of weather quantities this platform
// consumes.
//
// Closed for the same reason source.Kind is closed: an open vocabulary lets a
// misspelling become a silently empty series. "gust_10m" would store, read
// back as nothing, and produce a hazard calculation conditioned on a field
// that was never populated — indistinguishable from a calm.
//
// Each field carries its unit as a quantity.Unit rather than a string, so a
// hazard model cannot add a gust to an ambient temperature; IEEE 738 takes
// both. And each carries two ways of being found in a GRIB2 product: the
// abbreviation and level text its index sidecar keys on, and the numeric
// coordinates its section 4 carries.
//
// Both, because they are used at different moments. Locating a message to
// byte-range fetch it means matching the index's "GUST:surface"; confirming
// the bytes that arrive are the ones asked for means matching category 2,
// number 22, level type 1. A decoder that checked only one would fetch the
// right bytes and decode them as the wrong field, or the reverse.
//
// The table is per model. HRRR and GFS agree on parameter numbers and mostly
// on levels, but not always, and three fields exist in one and not the other.
package field

import (
	"errors"
	"fmt"
	"slices"

	"github.com/xraph/blackstart/extensions/weather/model"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/quantity"
)

// Field names one weather quantity.
type Field string

// The vocabulary.
//
// Corrected against real HRRR and GFS index files and a real HRRR message on
// 2026-08-03. The first version of this list was assembled from the WMO code
// tables alone and had the gust at the wrong level, the lightning threat at
// the wrong level, snow as a depth rather than an accumulation, and wind as a
// scalar that neither product publishes.
const (
	// Gust10m is the wind gust. Named for the height it represents even though
	// both products publish it at the surface level, because "the 10 m gust"
	// is what it is in every engineering document that consumes it.
	Gust10m Field = "gust10m"

	// WindU10m and WindV10m are the wind components at 10 m, the standard
	// observing height.
	//
	// Components rather than a speed, and not only because that is what the
	// products publish. A crosswind on a span is the component perpendicular
	// to it — which is why geo stores an azimuth per span — and a scalar speed
	// cannot produce one. Storing speed would discard the direction at ingest
	// and leave the azimuth with nothing to resolve against.
	WindU10m Field = "wind_u_10m"
	WindV10m Field = "wind_v_10m"

	// WindU80m and WindV80m are the wind components at 80 m, closer to
	// conductor height on a transmission structure than 10 m is.
	WindU80m Field = "wind_u_80m"
	WindV80m Field = "wind_v_80m"

	// Temp2m is the ambient temperature, an IEEE 738 input.
	Temp2m Field = "temp2m"

	// Dewpoint2m is the dewpoint, which with temperature gives the icing
	// regime.
	Dewpoint2m Field = "dewpoint2m"

	// RelHumidity is relative humidity at 2 m.
	RelHumidity Field = "rel_humidity"

	// PrecipRate is the precipitation mass flux.
	PrecipRate Field = "precip_rate"

	// PrecipRain, PrecipFreezingRain, PrecipIcePellets and PrecipSnow are the
	// categorical precipitation flags, as the products publish them: four
	// independent yes/no fields rather than one enumerated code.
	//
	// Kept separate because a model can report two at once on a transition
	// boundary, and that is a real state rather than a contradiction.
	// Collapsing them at ingest would hide it, and would make the stored value
	// an interpretation rather than a measurement.
	PrecipRain         Field = "precip_rain"
	PrecipFreezingRain Field = "precip_freezing_rain"
	PrecipIcePellets   Field = "precip_ice_pellets"
	PrecipSnow         Field = "precip_snow"

	// PercentFrozen is the percentage of precipitation falling frozen.
	PercentFrozen Field = "percent_frozen"

	// FreezingRain is accumulated freezing rain — the direct driver of the ice
	// load an accretion model computes. HRRR only.
	FreezingRain Field = "freezing_rain"

	// SnowAccum is accumulated snowfall. HRRR only.
	//
	// An accumulation rather than a depth, which is what the product actually
	// carries. The first version of this vocabulary asked for snow depth, a
	// different quantity in a different unit.
	SnowAccum Field = "snow_accum"

	// CloudCover is total cloud cover, which moderates the solar term in a
	// thermal rating.
	CloudCover Field = "cloud_cover"

	// Lightning is the lightning threat index. HRRR only.
	Lightning Field = "lightning"

	// CAPE is convective available potential energy — the storm-severity
	// proxy.
	CAPE Field = "cape"
)

// vocabulary is every field, in a fixed order.
//
//nolint:gochecknoglobals // a fixed vocabulary, not mutable state
var vocabulary = []Field{
	Gust10m,
	WindU10m, WindV10m, WindU80m, WindV80m,
	Temp2m, Dewpoint2m, RelHumidity,
	PrecipRate,
	PrecipRain, PrecipFreezingRain, PrecipIcePellets, PrecipSnow,
	PercentFrozen, FreezingRain, SnowAccum,
	CloudCover, Lightning, CAPE,
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

// ErrNotPublished means the field is in the vocabulary but the named model
// does not carry it.
//
// Distinct from an unknown field on purpose. Asking HRRR for lightning is
// ordinary; asking for "gust_10m" is a bug. An ingest pass skips the first and
// should fail on the second, so the two cannot be one error.
var ErrNotPublished = errors.New("not published by this model")

// Discipline is the GRIB2 discipline every field here belongs to.
//
// Zero is meteorological products. A constant rather than a per-field value
// because nothing in this vocabulary comes from any other discipline, and a
// column that is always the same invites a reader to wonder when it is not.
const Discipline uint8 = 0

// Spec is how to find a field in one model's product, and what its numbers
// mean.
type Spec struct {
	// Field and Model are what this spec describes.
	Field Field
	Model model.Model

	// Unit is what the source reports in.
	//
	// Not converted at ingest: a stored number that disagrees with the bytes
	// its provenance chain leads to is not much of a chain, and which
	// conversion a reader wants depends on what they are doing.
	Unit quantity.Unit

	// Abbrev and LevelText are what the index sidecar keys on — "GUST" and
	// "surface" for a line reading `9:3231216:d=…:GUST:surface:1 hour fcst:`.
	//
	// This is what locates a message for a byte-range fetch. The numeric
	// coordinates below do not appear in the index at all.
	Abbrev    string
	LevelText string

	// Category and Number are the WMO code table 4.2 coordinates within
	// Discipline, and LevelType is code table 4.5. They are what section 4 of
	// the fetched message carries, and therefore what confirms the bytes are
	// the ones that were asked for.
	Category  uint8
	Number    uint8
	LevelType uint8

	// LevelValue is the height in meters when LevelType is 103, and zero
	// otherwise. It is the only coordinate separating 10 m wind from 80 m
	// wind.
	LevelValue float64
}

// Level types from WMO code table 4.5, named so the table below reads.
const (
	levelSurface     uint8 = 1
	levelEntireAtmos uint8 = 10
	levelHeightAbove uint8 = 103
)

// shared is the spec table for fields both products publish identically.
//
// Verified against real index files from both models on 2026-08-03. GUST's
// numeric coordinates were additionally read from section 4 of a real HRRR
// message; the rest are from the published code tables and are confirmed by
// the decoder when a message arrives.
//
//nolint:gochecknoglobals // a fixed table, not mutable state
var shared = map[Field]Spec{
	Gust10m: {
		Unit: quantity.MeterPerSecond, Abbrev: "GUST", LevelText: "surface",
		Category: 2, Number: 22, LevelType: levelSurface,
	},
	WindU10m: {
		Unit: quantity.MeterPerSecond, Abbrev: "UGRD",
		LevelText: "10 m above ground",
		Category:  2, Number: 2, LevelType: levelHeightAbove, LevelValue: 10,
	},
	WindV10m: {
		Unit: quantity.MeterPerSecond, Abbrev: "VGRD",
		LevelText: "10 m above ground",
		Category:  2, Number: 3, LevelType: levelHeightAbove, LevelValue: 10,
	},
	WindU80m: {
		Unit: quantity.MeterPerSecond, Abbrev: "UGRD",
		LevelText: "80 m above ground",
		Category:  2, Number: 2, LevelType: levelHeightAbove, LevelValue: 80,
	},
	WindV80m: {
		Unit: quantity.MeterPerSecond, Abbrev: "VGRD",
		LevelText: "80 m above ground",
		Category:  2, Number: 3, LevelType: levelHeightAbove, LevelValue: 80,
	},
	Temp2m: {
		Unit: quantity.Kelvin, Abbrev: "TMP", LevelText: "2 m above ground",
		Category: 0, Number: 0, LevelType: levelHeightAbove, LevelValue: 2,
	},
	Dewpoint2m: {
		Unit: quantity.Kelvin, Abbrev: "DPT", LevelText: "2 m above ground",
		Category: 0, Number: 6, LevelType: levelHeightAbove, LevelValue: 2,
	},
	RelHumidity: {
		Unit: quantity.Percent, Abbrev: "RH", LevelText: "2 m above ground",
		Category: 1, Number: 1, LevelType: levelHeightAbove, LevelValue: 2,
	},
	PrecipRate: {
		Unit: quantity.KilogramPerSquareMeterSecond, Abbrev: "PRATE",
		LevelText: "surface",
		Category:  1, Number: 7, LevelType: levelSurface,
	},
	// The categorical flags carry the WMO standard numbers 33 to 36, not the
	// NCEP local-table numbers 192 to 195 that the abbreviations suggest.
	// Both exist; these products publish the former, which was read from the
	// section 4 of real messages rather than guessed from the abbreviation.
	//
	// Each is a yes/no value, so Ratio is the honest unit — giving one a
	// physical dimension would let it be added to something.
	PrecipRain: {
		Unit: quantity.Ratio, Abbrev: "CRAIN", LevelText: "surface",
		Category: 1, Number: 33, LevelType: levelSurface,
	},
	PrecipFreezingRain: {
		Unit: quantity.Ratio, Abbrev: "CFRZR", LevelText: "surface",
		Category: 1, Number: 34, LevelType: levelSurface,
	},
	PrecipIcePellets: {
		Unit: quantity.Ratio, Abbrev: "CICEP", LevelText: "surface",
		Category: 1, Number: 35, LevelType: levelSurface,
	},
	PrecipSnow: {
		Unit: quantity.Ratio, Abbrev: "CSNOW", LevelText: "surface",
		Category: 1, Number: 36, LevelType: levelSurface,
	},
	PercentFrozen: {
		Unit: quantity.Percent, Abbrev: "CPOFP", LevelText: "surface",
		Category: 1, Number: 39, LevelType: levelSurface,
	},
	CloudCover: {
		Unit: quantity.Percent, Abbrev: "TCDC", LevelText: "entire atmosphere",
		Category: 6, Number: 1, LevelType: levelEntireAtmos,
	},
	CAPE: {
		Unit: quantity.JoulePerKilogram, Abbrev: "CAPE", LevelText: "surface",
		Category: 7, Number: 6, LevelType: levelSurface,
	},
}

// hrrrOnly is the spec table for fields GFS does not carry.
//
// The 0.25-degree GFS product publishes no lightning threat, no freezing-rain
// accumulation and no snowfall accumulation. That is a fact about the
// products, checked against both index files rather than assumed.
//
//nolint:gochecknoglobals // a fixed table, not mutable state
var hrrrOnly = map[Field]Spec{
	Lightning: {
		Unit: quantity.Ratio, Abbrev: "LTNG", LevelText: "entire atmosphere",
		Category: 17, Number: 192, LevelType: levelEntireAtmos,
	},
	FreezingRain: {
		Unit: quantity.KilogramPerSquareMeter, Abbrev: "FRZR",
		LevelText: "surface",
		Category:  1, Number: 225, LevelType: levelSurface,
	},
	SnowAccum: {
		Unit: quantity.Meter, Abbrev: "ASNOW", LevelText: "surface",
		Category: 1, Number: 29, LevelType: levelSurface,
	},
}

// SpecFor returns how to find a field in one model's product.
//
// # Errors
//
// Returns an error wrapping bserr.ErrInvalid when the field is not in the
// vocabulary or the model is not one the platform knows, and one wrapping
// ErrNotPublished when the field is real but this model does not carry it.
// The two are distinct because skipping an absent field is ordinary and
// storing an unknown one is a bug.
func SpecFor(f Field, m model.Model) (Spec, error) {
	if !m.Valid() {
		return Spec{}, fmt.Errorf(
			"%w: %q is not a model this platform knows", bserr.ErrInvalid, m)
	}

	if !f.Valid() {
		return Spec{}, fmt.Errorf(
			"%w: %q is not a weather field this platform knows; the vocabulary "+
				"is closed so that a misspelling cannot become a silently empty "+
				"series", bserr.ErrInvalid, f)
	}

	if spec, ok := shared[f]; ok {
		spec.Field, spec.Model = f, m

		return spec, nil
	}

	if spec, ok := hrrrOnly[f]; ok && m == model.HRRR {
		spec.Field, spec.Model = f, m

		return spec, nil
	}

	return Spec{}, fmt.Errorf("%w: %s does not carry %q", ErrNotPublished, m, f)
}

// IndexKey renders what this spec matches in a product's index sidecar.
//
// The index line is `record:offset:date:ABBREV:LEVEL:forecast:`, so this is
// the middle pair a fetcher scans for.
func (s Spec) IndexKey() string { return s.Abbrev + ":" + s.LevelText }
