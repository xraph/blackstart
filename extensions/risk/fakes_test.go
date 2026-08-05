package risk_test

import (
	"context"
	"testing"
	"time"

	geoasset "github.com/xraph/blackstart/extensions/geo/asset"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	hzstore "github.com/xraph/blackstart/extensions/hazard/store"
	"github.com/xraph/blackstart/extensions/provenance"
	"github.com/xraph/blackstart/extensions/provenance/record"
	"github.com/xraph/blackstart/extensions/risk"
	"github.com/xraph/blackstart/extensions/risk/store/memory"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	"github.com/xraph/blackstart/pkg/id"
)

// issuance is the fixed instant every world's cycle is issued at, so a test
// compares stored values against a time that does not move.
func issuance() time.Time { return time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC) }

// fakeHazard answers with whatever hazard values a test has stashed for a
// span, filtered to the requested quantities and the [from, to) window.
//
// It ignores the requested issuance, the way hazard's own fakeWeather ignores
// it in SpanForecast: every fixture here has exactly one issuance in play, so
// filtering on it would only ever agree.
type fakeHazard struct {
	values map[id.ID][]hzstore.SpanValue
}

func (h *fakeHazard) SpanValues(
	_ context.Context, span id.ID, quantities []hzstore.Quantity, _, from, to time.Time,
) ([]hzstore.SpanValue, error) {
	want := make(map[hzstore.Quantity]bool, len(quantities))
	for _, q := range quantities {
		want[q] = true
	}

	out := make([]hzstore.SpanValue, 0)

	for _, v := range h.values[span] {
		if len(want) > 0 && !want[v.Quantity] {
			continue
		}

		if v.ValidAt.Before(from) || !v.ValidAt.Before(to) {
			continue
		}

		out = append(out, v)
	}

	return out, nil
}

// fakeWeather answers every request with the one cycle a world was built
// with, the way hazard's own fakeWeather does.
type fakeWeather struct {
	cycle forecast.Cycle
}

func (w *fakeWeather) Cycle(context.Context, id.ID) (forecast.Cycle, error) {
	return w.cycle, nil
}

// fakeGeometry is one route, its spans, and its structures.
type fakeGeometry struct {
	routes     []geoasset.Route
	spans      map[id.ID][]geoasset.Span
	structures []geoasset.Structure
}

func (g *fakeGeometry) Routes(context.Context, int) ([]geoasset.Route, error) {
	return g.routes, nil
}

func (g *fakeGeometry) Spans(_ context.Context, route id.ID) ([]geoasset.Span, error) {
	return g.spans[route], nil
}

func (g *fakeGeometry) Structures(_ context.Context, route id.ID) ([]geoasset.Structure, error) {
	out := make([]geoasset.Structure, 0, len(g.structures))

	for _, s := range g.structures {
		if s.RouteID == route {
			out = append(out, s)
		}
	}

	return out, nil
}

// fakeRecorder remembers what a pass recorded, the same double hazard's own
// tests use.
type fakeRecorder struct {
	model record.ModelVersion
	run   record.ProvenanceRun
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
	_ context.Context, in record.ProvenanceRun, _ []provenance.Input,
) (record.ProvenanceRun, error) {
	in.ID = id.MustNew("prvrun")
	r.run = in

	return in, nil
}

// world is a network of one route, two structures — a lattice tower and a
// pole — and the single span between them, over one weather cycle.
type world struct {
	store    *memory.Store
	hazard   *fakeHazard
	weather  *fakeWeather
	geometry *fakeGeometry
	recorder *fakeRecorder

	span     id.ID
	cycle    id.ID
	issuedAt time.Time
}

// newWorld builds the network described on [world], with no hazard values:
// callers that need them add withHazard.
func newWorld(t *testing.T) *world {
	t.Helper()

	route := id.MustNew(gid.Route)
	span := id.MustNew(gid.Span)
	cycle := id.MustNew("wxcyc")
	issuedAt := issuance()

	structures := []geoasset.Structure{
		{ID: gid.NewStructure(), RouteID: route, Ordinal: 0, Kind: "tower"},
		{ID: gid.NewStructure(), RouteID: route, Ordinal: 1, Kind: "pole"},
	}

	return &world{
		store:  memory.New(),
		hazard: &fakeHazard{values: map[id.ID][]hzstore.SpanValue{}},
		weather: &fakeWeather{
			cycle: forecast.Cycle{ID: cycle, IssuedAt: issuedAt},
		},
		geometry: &fakeGeometry{
			routes: []geoasset.Route{{
				ID: route, CarrierID: gid.NewCircuit(),
				Provenance: geoasset.Surveyed, LengthM: 300,
			}},
			spans: map[id.ID][]geoasset.Span{
				route: {{ID: span, RouteID: route, Sequence: 1, LengthM: 300}},
			},
			structures: structures,
		},
		recorder: &fakeRecorder{},
		span:     span,
		cycle:    cycle,
		issuedAt: issuedAt,
	}
}

// withHazard gives the world's one span wind and ice loading at each of three
// hours after issuance, the same shape hazard's own withWeather gives a span
// weather fields.
func (w *world) withHazard(loads map[hzstore.Quantity]float64) *world {
	values := make([]hzstore.SpanValue, 0, len(loads)*3)

	for hour := range 3 {
		at := w.issuedAt.Add(time.Duration(hour) * time.Hour)

		for quantity, value := range loads {
			values = append(values, hzstore.SpanValue{
				SpanID: w.span, Quantity: quantity,
				IssuedAt: w.issuedAt, ValidAt: at, Value: value,
			})
		}
	}

	w.hazard.values[w.span] = values

	return w
}

// icyLoading is wind and ice loading heavy enough that both structure curves
// evaluate to something visibly nonzero, so a clean pass exercises the full
// shape of the pass rather than its edge cases.
func icyLoading() map[hzstore.Quantity]float64 {
	return map[hzstore.Quantity]float64{
		hzstore.QuantityWindLoad: 60,
		hzstore.QuantityIceLoad:  3,
	}
}

func (w *world) engine(t *testing.T) *risk.Engine {
	t.Helper()

	e, err := risk.NewEngine(w.store, nil,
		risk.WithCompute(w.hazard, w.weather, w.geometry, w.recorder),
		risk.WithClock(issuance))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	return e
}
