package hazard

import (
	"math"
	"time"

	geoasset "github.com/xraph/blackstart/extensions/geo/asset"
	"github.com/xraph/blackstart/extensions/hazard/conductor"
	"github.com/xraph/blackstart/extensions/hazard/mechanical"
	"github.com/xraph/blackstart/extensions/hazard/store"
	"github.com/xraph/blackstart/extensions/hazard/thermal"
	"github.com/xraph/blackstart/extensions/weather/field"
)

// derived is one number a model produced.
type derived struct {
	quantity store.Quantity
	value    float64
}

// modelsAt runs every model over one span at one moment.
//
// Reports false when the moment has no ambient temperature. Temperature is the
// one field every model here needs — air density for drag, the freezing test
// for accretion, the heat balance for a rating — so a moment without it can
// produce nothing, and producing part of the set would leave a span with a wind
// load and no rating for reasons nothing downstream could reconstruct.
//
// Every other field is optional and its absence is treated as absence rather
// than as zero, with one deliberate exception noted at the ice call below.
func (e *Engine) modelsAt(
	which conductor.Conductor,
	span geoasset.Span,
	validAt time.Time,
	at map[field.Field]float64,
) ([]derived, bool) {
	tempK, ok := at[field.Temp2m]
	if !ok {
		return nil, false
	}

	out := make([]derived, 0, len(store.Quantities()))

	// Ice first: the wind model needs the sleeve's thickness to know how wide a
	// face it is pushing on.
	ice, iced := e.iceAt(which, at, tempK)
	if iced {
		out = append(out,
			derived{store.QuantityIceThickness, ice.ThicknessM},
			derived{store.QuantityIceLoad, ice.MassKgPerM},
		)
	}

	wind, blown := e.windAt(which, span, at, tempK, ice.ThicknessM)
	if blown {
		out = append(out, derived{store.QuantityWindLoad, wind.DragNPerM})
	}

	// Combined loading needs at least one of the two to be real. Two absences
	// would produce a resultant of zero, which is a claim of calm weather
	// rather than a statement that nothing was known.
	if iced || blown {
		combined := mechanical.Combined(wind, ice)
		out = append(out, derived{store.QuantityCombinedLoad, combined.MagnitudeNPerM})
	}

	if rating, rated := e.ratingAt(which, span, validAt, at, tempK); rated {
		out = append(out, derived{store.QuantityAmpacity, rating.AmpacityA})
	}

	return out, len(out) > 0
}

// iceAt runs the accretion model, if the moment says anything about ice.
//
// GFS publishes no freezing-rain accumulation, so a GFS-sourced pass produces
// no ice values at all. That is the correct answer rather than a gap to fill:
// the alternative is inventing an accumulation, and a fabricated ice load on a
// line nobody needs to derate is the expensive kind of wrong.
func (e *Engine) iceAt(
	which conductor.Conductor, at map[field.Field]float64, tempK float64,
) (mechanical.IceLoad, bool) {
	accumulation, ok := at[field.FreezingRain]
	if !ok {
		return mechanical.IceLoad{}, false
	}

	got, err := mechanical.Ice(mechanical.IceInput{
		FreezingRainKgPerM2: accumulation,
		// A missing rate means no wind enhancement rather than no accretion.
		// The collection term is a correction to a yield the accumulation
		// already gives, so its absence costs accuracy, not the value.
		PrecipRateKgPerM2S: at[field.PrecipRate],
		TempK:              tempK,
		WindMS:             speedAt(at),
		DiameterM:          which.DiameterM,
	})
	if err != nil {
		return mechanical.IceLoad{}, false
	}

	return got, true
}

// windAt runs the wind model, if the moment says anything about wind.
func (e *Engine) windAt(
	which conductor.Conductor,
	span geoasset.Span,
	at map[field.Field]float64,
	tempK, iceThicknessM float64,
) (mechanical.WindLoad, bool) {
	gust, ok := at[field.Gust10m]
	if !ok {
		// No gust published means loading against the mean, which understates
		// the peak but is far better than skipping the span. A zero mean too
		// and there is genuinely nothing to compute.
		gust = speedAt(at)
		if gust == 0 {
			return mechanical.WindLoad{}, false
		}
	}

	got, err := mechanical.Wind(mechanical.WindInput{
		GustMS:         gust,
		WindUMS:        at[field.WindU10m],
		WindVMS:        at[field.WindV10m],
		TempK:          tempK,
		SpanAzimuthDeg: span.AzimuthDeg,
		DiameterM:      which.DiameterM,
		IceThicknessM:  iceThicknessM,
	})
	if err != nil {
		return mechanical.WindLoad{}, false
	}

	return got, true
}

// ratingAt runs IEEE 738.
//
// Unlike the mechanical models this one always runs when there is a
// temperature: still air and a clear sky are a legitimate weather state and
// produce a legitimate rating, where the equivalent for wind loading would be
// a fabricated zero.
func (e *Engine) ratingAt(
	which conductor.Conductor,
	span geoasset.Span,
	validAt time.Time,
	at map[field.Field]float64,
	tempK float64,
) (thermal.Rating, bool) {
	center := midpoint(span.Geometry)

	// Cooling integrates over minutes — a conductor's thermal time constant is
	// ten to twenty of them — so the mean wind is the right input here, where
	// the mechanical models take the gust. A three-second gust does not cool a
	// conductor, and it breaks one instantly if it is going to.
	got, err := thermal.Ampacity(thermal.RatingInput{
		Conductor:         which,
		AmbientK:          tempK,
		WindMS:            speedAt(at),
		WindAngleDeg:      incidenceAt(at, span.AzimuthDeg),
		CloudCoverPercent: at[field.CloudCover],
		LatitudeDeg:       center.Lat,
		LongitudeDeg:      center.Lon,
		At:                validAt,
		ElevationM:        center.Elevation,
	})
	if err != nil {
		return thermal.Rating{}, false
	}

	return got, true
}

// speedAt is the mean wind speed from its components.
func speedAt(at map[field.Field]float64) float64 {
	return math.Hypot(at[field.WindU10m], at[field.WindV10m])
}

// incidenceAt is the angle between the wind and the span, 0 to 90 degrees.
//
// The same convention the mechanical package resolves a crosswind with: the
// meteorological direction is where the wind comes from, so the components are
// negated, and passing u before v converts counterclockwise-from-east into
// clockwise-from-north.
//
// A calm mean wind has no direction, and ninety degrees is the conservative
// reading — it is the angle that cools best, so assuming it would overstate the
// rating. But with a calm wind the thermal model falls to natural convection,
// where the angle does not enter at all, so the choice costs nothing.
func incidenceAt(at map[field.Field]float64, azimuthDeg float64) float64 {
	u, v := at[field.WindU10m], at[field.WindV10m]
	if u == 0 && v == 0 {
		return 90
	}

	const halfTurn = 180

	from := math.Atan2(-u, -v) * halfTurn / math.Pi

	folded := math.Abs(math.Mod(from-azimuthDeg, halfTurn))
	if folded > halfTurn/2 {
		folded = halfTurn - folded
	}

	return folded
}
