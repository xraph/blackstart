package hazard

import (
	"context"
	"errors"
	"fmt"
	"time"

	geoasset "github.com/xraph/blackstart/extensions/geo/asset"
	"github.com/xraph/blackstart/extensions/hazard/conductor"
	hid "github.com/xraph/blackstart/extensions/hazard/id"
	"github.com/xraph/blackstart/extensions/hazard/store"
	"github.com/xraph/blackstart/extensions/weather/field"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Weather is the part of the weather extension a computation needs.
//
// An interface here rather than the concrete engine, for the reason weather
// declares its own Geometry: this extension has no business writing forecasts,
// and a test of the compute pass should not need weather's storage compiled in.
//
// Satisfied by *weather.Engine.
type Weather interface {
	Cycle(ctx context.Context, want id.ID) (forecast.Cycle, error)
	Grids(ctx context.Context, cycle id.ID) ([]forecast.Grid, error)
	SpanForecast(
		ctx context.Context,
		span id.ID, fields []field.Field, issuedAt, from, to time.Time,
	) ([]forecast.Sample, error)
}

// Geometry is the part of the geo extension a computation needs.
//
// Route by identifier is what makes span → route → branch a traversal: a span
// carries its route, a route carries its branch, and the branch is what an
// assignment is keyed by. Satisfied by *geo.Engine.
type Geometry interface {
	Routes(ctx context.Context, limit int) ([]geoasset.Route, error)
	Route(ctx context.Context, want id.ID) (geoasset.Route, error)
	Spans(ctx context.Context, route id.ID) ([]geoasset.Span, error)
}

// ComputeRequest asks for one pass of the models over one weather cycle.
type ComputeRequest struct {
	// CycleID names the weather cycle to read.
	CycleID id.ID

	// Routes narrows the pass. Empty means every route the geometry knows.
	Routes []id.ID
}

// ComputeResult is what one pass did.
type ComputeResult struct {
	// ComputationID names the row recording this pass.
	ComputationID id.ID

	// RunID names the provenance run. Zero when the engine has no recorder.
	RunID id.ID

	// Spans is how many spans produced values.
	Spans int

	// Unrated is how many were skipped for want of a conductor assignment.
	//
	// Never a default conductor. A rating conditioned on a guessed conductor
	// looks exactly like one conditioned on a surveyed one, and the whole
	// point of the assignment basis is that they must not.
	Unrated int

	// Values is how many numbers were written.
	Values int

	// Covered and Total are span-hours that had enough weather to compute
	// something, and span-hours offered.
	//
	// Both, because HRRR is CONUS-only and a span outside its domain has no
	// samples at all: the difference between these two is the only place that
	// absence shows.
	Covered, Total int
}

// requiredFields are the weather fields the models read.
//
// Read as one set per span rather than per model, because a span's samples come
// back in one query and splitting it by model would multiply the round trips by
// four to save nothing.
//
//nolint:gochecknoglobals // a fixed input list, not mutable state
var requiredFields = []field.Field{
	field.Gust10m,
	field.WindU10m,
	field.WindV10m,
	field.Temp2m,
	field.PrecipRate,
	field.FreezingRain,
	field.CloudCover,
}

// Compute runs every model over every span of the given routes, for one weather
// cycle, and stores the results.
//
// The order inside a valid time is not arbitrary: ice accretes first, because
// the wind model needs the sleeve's thickness to know how wide a face it is
// pushing on. Combined loading follows both. The thermal rating is independent
// of all three and runs last only because that reads in the order an engineer
// would describe it.
//
// A span with no conductor assignment is counted unrated and contributes
// nothing — see [ComputeResult.Unrated].
//
// # Errors
//
// Returns an error when the engine was built for reads only, the cycle cannot
// be read, the geometry is unavailable, or the values cannot be stored — the
// conditions under which no span could succeed. A single span failing to read
// its weather is not one of them: it is counted uncovered.
func (e *Engine) Compute(
	ctx context.Context, in ComputeRequest,
) (ComputeResult, error) {
	if e.weather == nil || e.geometry == nil {
		return ComputeResult{}, errors.New(
			"hazard: computing needs a weather source and geometry; this engine " +
				"was built for reads only")
	}

	startedAt := e.now().UTC()

	cycle, err := e.weather.Cycle(ctx, in.CycleID)
	if err != nil {
		return ComputeResult{}, fmt.Errorf(
			"hazard: reading cycle %s: %w", in.CycleID, err)
	}

	spans, err := e.spansFor(ctx, in.Routes)
	if err != nil {
		return ComputeResult{}, err
	}

	if len(spans) == 0 {
		return ComputeResult{}, errors.New(
			"hazard: no spans to compute over; trace a route before computing a cycle")
	}

	window := e.horizon(ctx, cycle)

	result := ComputeResult{}
	values := make([]store.SpanValue, 0, len(spans)*len(store.Quantities()))

	for _, span := range spans {
		found, outcome, err := e.computeSpan(ctx, cycle, span, window)
		if err != nil {
			return result, err
		}

		switch {
		case outcome.unrated:
			result.Unrated++
		case len(found) > 0:
			result.Spans++
		}

		result.Covered += outcome.covered
		result.Total += outcome.total
		values = append(values, found...)
	}

	if err := e.store.Values().Write(ctx, values); err != nil {
		return result, err
	}

	result.Values = len(values)

	runID, err := e.recordRun(ctx, in.CycleID, startedAt)
	if err != nil {
		return result, err
	}

	result.RunID = runID
	result.ComputationID = hid.NewComputation()

	if err := e.store.Computations().Create(ctx, store.Computation{
		ID:         result.ComputationID,
		CycleID:    in.CycleID,
		RunID:      runID,
		ComputedAt: e.now().UTC(),
		Spans:      result.Spans,
		Unrated:    result.Unrated,
		Values:     result.Values,
	}); err != nil {
		return result, err
	}

	e.emit(ctx, "hazard.computation.completed", map[string]any{
		"computation_id": result.ComputationID.String(),
		"cycle_id":       in.CycleID.String(),
		"spans":          result.Spans,
		"unrated":        result.Unrated,
		"values":         result.Values,
	})

	return result, nil
}

// spanOutcome is one span's contribution to the counts.
type spanOutcome struct {
	// covered and total are the moments that produced values and the moments
	// offered.
	covered, total int

	// unrated marks a span whose branch has no conductor assignment. Distinct
	// from a span that produced nothing for want of weather: one is a gap in
	// the asset record and the other a gap in the forecast domain, and a single
	// count would let a network nobody has surveyed look like bad weather.
	unrated bool
}

// computeSpan runs the models over one span for every valid time it has
// weather at.
func (e *Engine) computeSpan(
	ctx context.Context,
	cycle forecast.Cycle,
	span geoasset.Span,
	window timeWindow,
) ([]store.SpanValue, spanOutcome, error) {
	which, assigned, err := e.conductorFor(ctx, span)
	if err != nil {
		return nil, spanOutcome{}, err
	}

	if !assigned {
		return nil, spanOutcome{unrated: true}, nil
	}

	samples, err := e.weather.SpanForecast(
		ctx, span.ID, requiredFields, cycle.IssuedAt, window.from, window.to)
	if err != nil {
		return nil, spanOutcome{}, fmt.Errorf(
			"hazard: reading weather for span %s: %w", span.ID, err)
	}

	byTime := groupByValidTime(samples)
	out := make([]store.SpanValue, 0, len(byTime)*len(store.Quantities()))

	outcome := spanOutcome{total: len(byTime)}

	for _, validAt := range sortedTimes(byTime) {
		at := byTime[validAt]

		computed, ok := e.modelsAt(which, span, validAt, at)
		if !ok {
			continue
		}

		outcome.covered++

		for _, one := range computed {
			out = append(out, store.SpanValue{
				SpanID:   span.ID,
				Quantity: one.quantity,
				IssuedAt: cycle.IssuedAt,
				ValidAt:  validAt,
				Value:    one.value,
			})
		}
	}

	// A span offered no valid times at all is a span outside the model's
	// domain, which is coverage information rather than a failure.
	if outcome.total == 0 {
		outcome.total = 1
	}

	return out, outcome, nil
}

// conductorFor resolves span → route → branch → assignment → conductor.
//
// Reports false, with no error, when the branch has no assignment. That is the
// unrated case and it is deliberately not an error, because it is a gap in the
// asset record rather than a fault.
//
// A missing route is an error, and the distinction is the point: both surface
// as bserr.ErrNotFound from their stores, and collapsing them would let a
// broken route reference be counted as a network nobody has surveyed.
func (e *Engine) conductorFor(
	ctx context.Context, span geoasset.Span,
) (conductor.Conductor, bool, error) {
	route, err := e.geometry.Route(ctx, span.RouteID)
	if err != nil {
		return conductor.Conductor{}, false, fmt.Errorf(
			"hazard: reading route %s for span %s: %w", span.RouteID, span.ID, err)
	}

	assignment, err := e.store.Assignments().ForBranch(ctx, route.BranchID)
	if err != nil {
		if errors.Is(err, bserr.ErrNotFound) {
			return conductor.Conductor{}, false, nil
		}

		return conductor.Conductor{}, false, err
	}

	which, err := conductor.ByCode(assignment.ConductorCode)
	if err != nil {
		return conductor.Conductor{}, false, fmt.Errorf(
			"hazard: assignment %s on branch %s: %w",
			assignment.ID, route.BranchID, err)
	}

	return which, true, nil
}
