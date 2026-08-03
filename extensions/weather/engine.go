// Package weather owns what the atmosphere was doing, and when we learned it.
//
// The storage half of Phase 3. It records model runs and the GRIB2 retrievals
// that make them up, and it stores the per-span samples a hazard model
// conditions on. It does not fetch anything yet and it computes nothing
// meteorological — wind loading, ice accretion and IEEE 738 belong to the
// hazard extension, and the ingest pipeline that fills these tables is the
// next increment.
//
// Two things are load-bearing throughout. Every retrieval names the dataset
// version that recorded it, so a number traces back to the bytes it came from.
// And every sample carries both an issuance and a valid time, because a
// forecast revised after the fact is the most common way an outage model comes
// to have seen information that did not exist when it claims to have predicted.
package weather

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xraph/blackstart/extensions/weather/field"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	"github.com/xraph/blackstart/extensions/weather/observation"
	"github.com/xraph/blackstart/extensions/weather/store"
	"github.com/xraph/blackstart/pkg/hooks"
	"github.com/xraph/blackstart/pkg/id"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

// Engine holds the extension's business logic.
type Engine struct {
	store store.Store
	bus   *hooks.Bus
}

// NewEngine builds the engine.
//
// The bus may be nil — a CLI invocation has no interest in domain events.
func NewEngine(s store.Store, bus *hooks.Bus) (*Engine, error) {
	if s == nil {
		return nil, errors.New("weather: engine needs a store")
	}

	return &Engine{store: s, bus: bus}, nil
}

// Cycle reads one model run.
func (e *Engine) Cycle(ctx context.Context, want id.ID) (forecast.Cycle, error) {
	return e.store.Cycles().Get(ctx, want)
}

// Cycles lists model runs, newest issuance first.
func (e *Engine) Cycles(
	ctx context.Context, f store.CycleFilter, p pkgstore.Page,
) ([]forecast.Cycle, error) {
	return e.store.Cycles().List(ctx, f, p)
}

// CycleAt finds the run of a model at an issuance.
//
// The question the ingest pipeline asks before doing anything else, and the
// reason a backfill pass is cheap to run on every restart: a cycle already
// recorded costs one lookup.
func (e *Engine) CycleAt(
	ctx context.Context, model forecast.Model, issuedAt time.Time,
) (forecast.Cycle, error) {
	return e.store.Cycles().ForIssuance(ctx, model, issuedAt)
}

// Grids reads every retrieval of a cycle, by forecast hour then field.
func (e *Engine) Grids(ctx context.Context, cycle id.ID) ([]forecast.Grid, error) {
	return e.store.Grids().ForCycle(ctx, cycle)
}

// SpanForecast reads what a model expected at one span over a window.
//
// The window is half-open. Issuance is a parameter rather than being inferred
// from the window, because "what does the latest run say" and "what did the
// 06Z run say" are different questions, and a reader who cannot ask the second
// one cannot check a forecast against what actually happened.
func (e *Engine) SpanForecast(
	ctx context.Context,
	span id.ID, fields []field.Field, issuedAt, from, to time.Time,
) ([]forecast.Sample, error) {
	if span.IsZero() {
		return nil, errors.New("weather: reading a forecast needs a span")
	}

	if len(fields) == 0 {
		fields = field.Fields()
	}

	keys := make([]string, 0, len(fields))

	for _, want := range fields {
		if !want.Valid() {
			return nil, fmt.Errorf(
				"weather: %q is not a field in the vocabulary", want)
		}

		keys = append(keys, forecast.SampleKey(span, want, issuedAt))
	}

	return e.store.Samples().ReadForecast(ctx, keys, from, to)
}

// StationObservations reads what a station measured over a window.
func (e *Engine) StationObservations(
	ctx context.Context, station id.ID, fields []field.Field, from, to time.Time,
) ([]observation.Observation, error) {
	if station.IsZero() {
		return nil, errors.New("weather: reading observations needs a station")
	}

	if len(fields) == 0 {
		fields = field.Fields()
	}

	keys := make([]string, 0, len(fields))

	for _, want := range fields {
		if !want.Valid() {
			return nil, fmt.Errorf(
				"weather: %q is not a field in the vocabulary", want)
		}

		keys = append(keys, observation.SampleKey(station, want))
	}

	return e.store.Samples().ReadObservations(ctx, keys, from, to)
}

// Station reads one observing site.
func (e *Engine) Station(
	ctx context.Context, want id.ID,
) (observation.Station, error) {
	return e.store.Stations().Get(ctx, want)
}

// Stations lists observing sites.
func (e *Engine) Stations(
	ctx context.Context, p pkgstore.Page,
) ([]observation.Station, error) {
	return e.store.Stations().List(ctx, p)
}

// RecordCycle notes a model run, returning the existing one when it is already
// known.
//
// Idempotent on purpose. Bounded backfill re-walks recent issuances on every
// worker restart, so this is called far more often with a cycle that exists
// than with one that does not, and making the repeat an error would mean every
// restart logged failures for working correctly.
func (e *Engine) RecordCycle(
	ctx context.Context, in forecast.Cycle,
) (forecast.Cycle, error) {
	existing, err := e.store.Cycles().ForIssuance(ctx, in.Model, in.IssuedAt)
	if err == nil {
		return existing, nil
	}

	if err := e.store.Cycles().Create(ctx, in); err != nil {
		return forecast.Cycle{}, err
	}

	e.emit(ctx, "weather.cycle.recorded", "Cycle", in.ID, map[string]any{
		"model":     string(in.Model),
		"issued_at": in.IssuedAt.UTC().Format(time.RFC3339),
	})

	return in, nil
}

// emit publishes a domain event, best effort.
func (e *Engine) emit(
	ctx context.Context, name, entity string, entityID id.ID, payload map[string]any,
) {
	if e.bus == nil {
		return
	}

	e.bus.Emit(ctx, hooks.Event{
		Name:      name,
		Extension: ExtensionName,
		Entity:    entity,
		EntityID:  entityID.String(),
		Op:        hooks.OpCreate,
		Payload:   payload,
	})
}
