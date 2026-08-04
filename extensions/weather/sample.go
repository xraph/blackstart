package weather

import (
	"context"
	"errors"
	"fmt"

	geoasset "github.com/xraph/blackstart/extensions/geo/asset"
	"github.com/xraph/blackstart/extensions/geo/shape"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/solver"
)

// Geometry is the part of the geo extension sampling needs.
//
// An interface here rather than the concrete engine, for the reason the ingest
// half declares Feeds: this extension has no business writing geometry, and a
// test of the sampling pass should not need geo's storage compiled in.
type Geometry interface {
	Routes(ctx context.Context, limit int) ([]geoasset.Route, error)
	Spans(ctx context.Context, route id.ID) ([]geoasset.Span, error)
}

// Grids is the part of the grid service sampling needs. Satisfied by
// *solver.GridClient.
type Grids interface {
	UploadGrid(ctx context.Context, message []byte) (string, solver.GridDescription, error)
	SampleGrid(ctx context.Context, hash string, points []solver.GridPoint) ([]solver.GridSample, error)
}

// SampleResult is what one sampling pass did.
type SampleResult struct {
	// Grids is how many of the cycle's grids were sampled.
	Grids int

	// Samples is how many values were written.
	Samples int

	// Covered and Total are spans inside the grid's domain, and spans offered,
	// summed over every grid.
	//
	// Both, because HRRR is CONUS-only: a span outside it yields no sample at
	// all, and the difference between these two is the only place that shows.
	Covered int
	Total   int
}

// SampleCycle samples every grid of a cycle onto the spans of the given
// routes.
//
// Passing no routes samples every route the geometry knows.
//
// # Errors
//
// Returns an error when the cycle cannot be read, the geometry is
// unavailable, or the grid service cannot be reached — the conditions under
// which no grid could succeed.
func (e *Engine) SampleCycle(
	ctx context.Context, cycleID id.ID, routes []id.ID,
) (SampleResult, error) {
	if e.geometry == nil || e.grids == nil {
		return SampleResult{}, errors.New(
			"weather: sampling needs geometry and a grid service; this engine was " +
				"built for reads only")
	}

	cycle, err := e.store.Cycles().Get(ctx, cycleID)
	if err != nil {
		return SampleResult{}, err
	}

	spans, err := e.spansFor(ctx, routes)
	if err != nil {
		return SampleResult{}, err
	}

	if len(spans) == 0 {
		return SampleResult{}, errors.New(
			"weather: no spans to sample; trace a route before sampling a cycle")
	}

	points := make([]solver.GridPoint, 0, len(spans))

	for _, span := range spans {
		center := midpoint(span.Geometry)
		points = append(points, solver.GridPoint{Lon: center.Lon, Lat: center.Lat})
	}

	grids, err := e.store.Grids().ForCycle(ctx, cycleID)
	if err != nil {
		return SampleResult{}, err
	}

	var result SampleResult

	for _, grid := range grids {
		sampled, err := e.sampleGrid(ctx, cycle, grid, spans, points)
		if err != nil {
			return result, err
		}

		result.Grids++
		result.Samples += sampled.written
		result.Covered += sampled.covered
		result.Total += len(spans)
	}

	if result.Grids > 0 {
		if err := e.store.Cycles().SetStatus(
			ctx, cycleID, forecast.StatusSampled,
		); err != nil {
			return result, err
		}
	}

	return result, nil
}

// gridOutcome is one grid's contribution.
type gridOutcome struct {
	written int
	covered int
}

// sampleGrid reads one grid's bytes, samples them, and stores the result.
func (e *Engine) sampleGrid(
	ctx context.Context,
	cycle forecast.Cycle,
	grid forecast.Grid,
	spans []geoasset.Span,
	points []solver.GridPoint,
) (gridOutcome, error) {
	body, err := e.store.Blobs().Get(ctx, grid.BlobRef)
	if err != nil {
		return gridOutcome{}, fmt.Errorf(
			"weather: reading the bytes for grid %s: %w", grid.ID, err)
	}

	hash, _, err := e.grids.UploadGrid(ctx, body)
	if err != nil {
		return gridOutcome{}, fmt.Errorf(
			"weather: uploading grid %s: %w", grid.ID, err)
	}

	got, err := e.grids.SampleGrid(ctx, hash, points)
	if err != nil {
		return gridOutcome{}, fmt.Errorf(
			"weather: sampling grid %s: %w", grid.ID, err)
	}

	if len(got) != len(spans) {
		return gridOutcome{}, fmt.Errorf(
			"weather: grid %s returned %d samples for %d spans; results pair by "+
				"position, so this would attribute weather to the wrong spans",
			grid.ID, len(got), len(spans))
	}

	validAt := grid.ValidAt(cycle.IssuedAt)
	samples := make([]forecast.Sample, 0, len(got))

	for index, sample := range got {
		// Absence is absence. A span outside the domain contributes no sample
		// rather than a zero, because a zero here is a fabricated calm that
		// nothing downstream could tell from a real one.
		if !sample.Present {
			continue
		}

		samples = append(samples, forecast.Sample{
			SpanID:   spans[index].ID,
			Field:    grid.Field,
			IssuedAt: cycle.IssuedAt,
			ValidAt:  validAt,
			Value:    sample.Value,
		})
	}

	if err := e.store.Samples().WriteForecast(ctx, samples); err != nil {
		return gridOutcome{}, err
	}

	if err := e.store.Grids().MarkSampled(
		ctx, grid.ID, e.now().UTC(), len(samples), len(spans),
	); err != nil {
		return gridOutcome{}, err
	}

	return gridOutcome{written: len(samples), covered: len(samples)}, nil
}

// spansFor collects the spans of the named routes, or of every route.
func (e *Engine) spansFor(
	ctx context.Context, routes []id.ID,
) ([]geoasset.Span, error) {
	if len(routes) == 0 {
		// No limit is expressed as a large one: the geometry engine's Routes
		// takes a count, and a weather pass wants all of them.
		const everyRoute = 10000

		found, err := e.geometry.Routes(ctx, everyRoute)
		if err != nil {
			return nil, fmt.Errorf("weather: listing routes: %w", err)
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
			return nil, fmt.Errorf("weather: reading spans of %s: %w", route, err)
		}

		out = append(out, spans...)
	}

	return out, nil
}

// midpoint is where along a span the weather is sampled.
//
// A span is a few hundred meters and a HRRR cell is three kilometers, so which
// point of the span is used matters far less than that it lands in the right
// cell. The midpoint is the least arbitrary choice available and is stable
// across re-samples, which matters more: a sample point that moved between
// passes would make two runs of the same cycle disagree for no physical
// reason.
func midpoint(line shape.Line) shape.Point {
	if len(line) == 0 {
		return shape.Point{}
	}

	if len(line)%2 == 1 {
		return line[len(line)/2]
	}

	// An even number of points has no middle one, so the two nearest are
	// averaged. Linear interpolation over a few hundred meters is well inside
	// the error the grid resolution already imposes.
	low, high := line[len(line)/2-1], line[len(line)/2]

	return shape.Point{
		Lon:       (low.Lon + high.Lon) / 2,
		Lat:       (low.Lat + high.Lat) / 2,
		Elevation: (low.Elevation + high.Elevation) / 2,
	}
}
