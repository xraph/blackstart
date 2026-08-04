package hazard

import (
	"context"
	"fmt"
	"slices"
	"time"

	geoasset "github.com/xraph/blackstart/extensions/geo/asset"
	"github.com/xraph/blackstart/extensions/geo/shape"
	"github.com/xraph/blackstart/extensions/weather/field"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	"github.com/xraph/blackstart/pkg/id"
)

// timeWindow bounds a computation pass.
type timeWindow struct{ from, to time.Time }

// horizon is the window of valid times a cycle covers.
//
// Derived from the cycle's own grids rather than from the model's nominal
// horizon, because a cycle that was only partly fetched has a shorter one and
// asking for hours it does not have would read nothing and look like absence.
// A cycle whose grids cannot be listed falls back to the widest window the
// models publish, which over-asks and costs one empty query.
func (e *Engine) horizon(ctx context.Context, cycle forecast.Cycle) timeWindow {
	// Twenty-one days, past the longest horizon either model publishes.
	const widest = 21 * 24 * time.Hour

	fallback := timeWindow{
		from: cycle.IssuedAt,
		to:   cycle.IssuedAt.Add(widest),
	}

	grids, err := e.weather.Grids(ctx, cycle.ID)
	if err != nil || len(grids) == 0 {
		return fallback
	}

	last := 0
	for _, grid := range grids {
		last = max(last, grid.ForecastHour)
	}

	// The upper bound is exclusive, so the last hour needs one past it.
	return timeWindow{
		from: cycle.IssuedAt,
		to:   cycle.IssuedAt.Add(time.Duration(last+1) * time.Hour),
	}
}

// spansFor collects the spans of the named routes, or of every route.
func (e *Engine) spansFor(
	ctx context.Context, routes []id.ID,
) ([]geoasset.Span, error) {
	if len(routes) == 0 {
		// No limit is expressed as a large one: the geometry engine's Routes
		// takes a count, and a computation pass wants all of them.
		const everyRoute = 10000

		found, err := e.geometry.Routes(ctx, everyRoute)
		if err != nil {
			return nil, fmt.Errorf("hazard: listing routes: %w", err)
		}

		routes = make([]id.ID, 0, len(found))

		for _, route := range found {
			routes = append(routes, route.ID)
		}
	}

	var out []geoasset.Span

	for _, route := range routes {
		spans, err := e.geometry.Spans(ctx, route)
		if err != nil {
			return nil, fmt.Errorf("hazard: reading spans of %s: %w", route, err)
		}

		out = append(out, spans...)
	}

	return out, nil
}

// groupByValidTime collects a span's samples into one bundle per moment.
//
// The models take a complete weather state at an instant, so the samples have
// to be regrouped from the per-field series the store returns.
func groupByValidTime(samples []forecast.Sample) map[time.Time]map[field.Field]float64 {
	out := map[time.Time]map[field.Field]float64{}

	for _, sample := range samples {
		at := sample.ValidAt.UTC()

		if out[at] == nil {
			out[at] = map[field.Field]float64{}
		}

		out[at][sample.Field] = sample.Value
	}

	return out
}

// sortedTimes orders the moments so a pass writes values in time order and two
// runs of the same cycle produce the same sequence.
func sortedTimes(byTime map[time.Time]map[field.Field]float64) []time.Time {
	out := make([]time.Time, 0, len(byTime))
	for at := range byTime {
		out = append(out, at)
	}

	slices.SortFunc(out, func(left, right time.Time) int { return left.Compare(right) })

	return out
}

// midpoint is where along a span the sun's position and elevation are taken.
//
// The same choice the weather extension makes for its sample point, and for the
// same reason: a span is a few hundred meters, the quantities that depend on
// position vary over tens of kilometers, and stability across re-runs matters
// far more than which point is used.
func midpoint(line shape.Line) shape.Point {
	if len(line) == 0 {
		return shape.Point{}
	}

	if len(line)%2 == 1 {
		return line[len(line)/2]
	}

	low, high := line[len(line)/2-1], line[len(line)/2]

	return shape.Point{
		Lon:       (low.Lon + high.Lon) / 2,
		Lat:       (low.Lat + high.Lat) / 2,
		Elevation: (low.Elevation + high.Elevation) / 2,
	}
}
