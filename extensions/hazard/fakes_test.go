package hazard_test

import (
	"context"
	"testing"
	"time"

	geoasset "github.com/xraph/blackstart/extensions/geo/asset"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/geo/shape"
	"github.com/xraph/blackstart/extensions/hazard"
	"github.com/xraph/blackstart/extensions/hazard/conductor"
	hid "github.com/xraph/blackstart/extensions/hazard/id"
	"github.com/xraph/blackstart/extensions/hazard/store/memory"
	"github.com/xraph/blackstart/extensions/provenance"
	"github.com/xraph/blackstart/extensions/provenance/record"
	"github.com/xraph/blackstart/extensions/weather/field"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	wxstore "github.com/xraph/blackstart/extensions/weather/store"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

func issuance() time.Time { return time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC) }

// fakeWeather is one cycle, a fixed set of grids, and whatever samples a test
// hands it, keyed by span.
type fakeWeather struct {
	cycle   forecast.Cycle
	grids   []forecast.Grid
	samples map[id.ID][]forecast.Sample
}

func (w *fakeWeather) Cycle(context.Context, id.ID) (forecast.Cycle, error) {
	return w.cycle, nil
}

func (w *fakeWeather) Grids(context.Context, id.ID) ([]forecast.Grid, error) {
	return w.grids, nil
}

// Cycles answers with the one cycle when its status matches the filter, which
// is all the recompute worker asks of it.
func (w *fakeWeather) Cycles(
	_ context.Context, f wxstore.CycleFilter, _ pkgstore.Page,
) ([]forecast.Cycle, error) {
	if f.Status != "" && f.Status != w.cycle.Status {
		return nil, nil
	}

	return []forecast.Cycle{w.cycle}, nil
}

func (w *fakeWeather) SpanForecast(
	_ context.Context, span id.ID, _ []field.Field, _, from, to time.Time,
) ([]forecast.Sample, error) {
	out := make([]forecast.Sample, 0)

	for _, sample := range w.samples[span] {
		if !sample.ValidAt.Before(from) && sample.ValidAt.Before(to) {
			out = append(out, sample)
		}
	}

	return out, nil
}

// fakeGeometry is a handful of routes and their spans.
type fakeGeometry struct {
	routes []geoasset.Route
	spans  map[id.ID][]geoasset.Span
}

func (g *fakeGeometry) Routes(context.Context, int) ([]geoasset.Route, error) {
	return g.routes, nil
}

func (g *fakeGeometry) Route(_ context.Context, want id.ID) (geoasset.Route, error) {
	for _, route := range g.routes {
		if route.ID == want {
			return route, nil
		}
	}

	return geoasset.Route{}, bserr.ErrNotFound
}

func (g *fakeGeometry) Spans(_ context.Context, route id.ID) ([]geoasset.Span, error) {
	return g.spans[route], nil
}

// fakeRecorder remembers what a pass recorded.
type fakeRecorder struct {
	model  record.ModelVersion
	run    record.Run
	inputs []provenance.Input
}

func (r *fakeRecorder) RecordDataset(
	_ context.Context, in record.DatasetVersion,
) (record.DatasetVersion, error) {
	in.ID = id.MustNew("prvds")

	return in, nil
}

func (r *fakeRecorder) RecordModel(
	_ context.Context, in record.ModelVersion,
) (record.ModelVersion, error) {
	in.ID = id.MustNew("prvmv")
	r.model = in

	return in, nil
}

func (r *fakeRecorder) RecordRun(
	_ context.Context, in record.Run, inputs []provenance.Input,
) (record.Run, error) {
	in.ID = id.MustNew("prvrun")
	r.run, r.inputs = in, inputs

	return in, nil
}

// world is a network of one route and one span, with weather over it.
type world struct {
	store    *memory.Store
	weather  *fakeWeather
	geometry *fakeGeometry
	recorder *fakeRecorder

	branch, route, span id.ID
	datasetVersion      id.ID
}

// newWorld builds a network with one traced route and a cycle of HRRR grids
// over it, and assigns Drake to the branch.
func newWorld(t *testing.T) *world {
	t.Helper()

	w := &world{
		store:          memory.New(),
		recorder:       &fakeRecorder{},
		branch:         id.MustNew("brch"),
		route:          id.MustNew(gid.Route),
		span:           id.MustNew(gid.Span),
		datasetVersion: id.MustNew("prvds"),
	}

	cycleID := id.MustNew("wxcyc")

	w.weather = &fakeWeather{
		cycle: forecast.Cycle{
			ID: cycleID, Model: forecast.ModelHRRR, IssuedAt: issuance(),
		},
		grids: []forecast.Grid{
			{ForecastHour: 0, DatasetVersionID: w.datasetVersion},
			{ForecastHour: 1, DatasetVersionID: w.datasetVersion},
			{ForecastHour: 2, DatasetVersionID: w.datasetVersion},
		},
		samples: map[id.ID][]forecast.Sample{},
	}

	w.geometry = &fakeGeometry{
		routes: []geoasset.Route{{ID: w.route, CarrierID: w.branch}},
		spans: map[id.ID][]geoasset.Span{
			w.route: {{
				ID: w.span, RouteID: w.route, Sequence: 0,
				Geometry: shape.Line{
					{Lon: -97.7, Lat: 30.3}, {Lon: -97.6, Lat: 30.4},
				},
				LengthM: 300, AzimuthDeg: 45,
			}},
		},
	}

	if err := w.store.Assignments().Put(t.Context(), conductor.Assignment{
		ID:            hid.NewAssignment(),
		BranchID:      w.branch,
		ConductorCode: "drake",
		Basis:         conductor.Surveyed,
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	return w
}

// withWeather gives the span a full set of fields at each of three hours.
func (w *world) withWeather(fields map[field.Field]float64) *world {
	samples := make([]forecast.Sample, 0, len(fields)*3)

	for hour := range 3 {
		at := issuance().Add(time.Duration(hour) * time.Hour)

		for got, value := range fields {
			samples = append(samples, forecast.Sample{
				SpanID: w.span, Field: got, IssuedAt: issuance(),
				ValidAt: at, Value: value,
			})
		}
	}

	w.weather.samples[w.span] = samples

	return w
}

func (w *world) engine(t *testing.T) *hazard.Engine {
	t.Helper()

	e, err := hazard.NewEngine(w.store, nil,
		hazard.WithCompute(w.weather, w.geometry, w.recorder),
		hazard.WithClock(issuance))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	return e
}

func icyWeather() map[field.Field]float64 {
	return map[field.Field]float64{
		field.Temp2m:       270,
		field.Gust10m:      22,
		field.WindU10m:     12,
		field.WindV10m:     4,
		field.PrecipRate:   5e-4,
		field.FreezingRain: 8,
		field.CloudCover:   40,
	}
}
