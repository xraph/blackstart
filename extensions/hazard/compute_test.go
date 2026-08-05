package hazard_test

import (
	"errors"
	"testing"
	"time"

	geoasset "github.com/xraph/blackstart/extensions/geo/asset"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/hazard"
	"github.com/xraph/blackstart/extensions/hazard/conductor"
	hid "github.com/xraph/blackstart/extensions/hazard/id"
	hzstore "github.com/xraph/blackstart/extensions/hazard/store"
	"github.com/xraph/blackstart/extensions/hazard/store/memory"
	"github.com/xraph/blackstart/extensions/weather/field"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

// A clean pass writes every quantity, at every hour, for the one span.
func TestACleanPassWritesEveryQuantity(t *testing.T) {
	t.Parallel()

	w := newWorld(t).withWeather(icyWeather())
	e := w.engine(t)

	got, err := e.Compute(t.Context(), hazard.ComputeRequest{CycleID: w.weather.cycle.ID})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	if got.Spans != 1 || got.Unrated != 0 {
		t.Errorf("counted %d spans and %d unrated, want 1 and 0", got.Spans, got.Unrated)
	}

	if want := len(hzstore.Quantities()) * 3; got.Values != want {
		t.Errorf("wrote %d values, want %d", got.Values, want)
	}

	for _, quantity := range hzstore.Quantities() {
		values, err := e.SpanValues(t.Context(), w.span,
			[]hzstore.Quantity{quantity}, issuance(),
			issuance(), issuance().Add(6*time.Hour))
		if err != nil {
			t.Fatalf("SpanValues(%s): %v", quantity, err)
		}

		if len(values) != 3 {
			t.Errorf("%s has %d values, want one an hour for three hours",
				quantity, len(values))
		}
	}
}

// A span whose branch has no conductor produces nothing and is counted. Never a
// default conductor: a rating conditioned on a guess looks exactly like one
// conditioned on a survey, and this is the line that keeps them apart.
func TestAnUnassignedSpanIsCountedUnrated(t *testing.T) {
	t.Parallel()

	w := newWorld(t).withWeather(icyWeather())

	assignments, err := w.store.Assignments().List(t.Context(), pkgstore.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	for _, existing := range assignments {
		if err := w.store.Assignments().Delete(t.Context(), existing.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
	}

	got, err := w.engine(t).Compute(
		t.Context(), hazard.ComputeRequest{CycleID: w.weather.cycle.ID})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	if got.Unrated != 1 || got.Spans != 0 {
		t.Errorf("counted %d unrated and %d rated, want 1 and 0", got.Unrated, got.Spans)
	}

	if got.Values != 0 {
		t.Errorf("an unassigned span produced %d values", got.Values)
	}
}

// A route may carry a branch in a network model or a real circuit, and both
// are made of conductor hanging in the same weather. IEEE 738 does not ask
// which model an element came from, so a circuit with a conductor assigned
// rates exactly as a branch does.
//
// This test used to assert the opposite. A guard in conductorFor returned
// early for any circuit, because asking ForBranch with a circuit identifier
// missed, and that miss read as "nobody assigned a conductor yet" — a category
// error indistinguishable from a gap in the record. That reasoning was right
// while assignments keyed on branches. Keyed on carriers, a circuit without an
// assignment is simply an unassigned carrier, so the miss means what it says
// and there is no longer a category to confuse.
func TestASpanOnAnAssignedCircuitIsRated(t *testing.T) {
	t.Parallel()

	w := newWorld(t).withWeather(icyWeather())

	circuit := id.MustNew(gid.Circuit)
	w.geometry.routes = []geoasset.Route{{ID: w.route, CarrierID: circuit}}

	if err := w.store.Assignments().Put(t.Context(), conductor.Assignment{
		ID:            hid.NewAssignment(),
		CarrierID:     circuit,
		ConductorCode: "drake",
		Basis:         conductor.AssumedByVoltage,
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := w.engine(t).Compute(
		t.Context(), hazard.ComputeRequest{CycleID: w.weather.cycle.ID})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	if got.Spans != 1 || got.Unrated != 0 {
		t.Errorf("counted %d rated and %d unrated, want 1 and 0", got.Spans, got.Unrated)
	}

	if got.Values == 0 {
		t.Error("an assigned circuit produced no values")
	}
}

// The concern the old guard protected, kept: a circuit nobody has assigned a
// conductor to is unrated, and is counted as such rather than failing the pass.
func TestASpanOnAnUnassignedCircuitIsCountedUnrated(t *testing.T) {
	t.Parallel()

	w := newWorld(t).withWeather(icyWeather())

	w.geometry.routes = []geoasset.Route{
		{ID: w.route, CarrierID: id.MustNew(gid.Circuit)},
	}

	got, err := w.engine(t).Compute(
		t.Context(), hazard.ComputeRequest{CycleID: w.weather.cycle.ID})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	if got.Unrated != 1 || got.Spans != 0 {
		t.Errorf("counted %d unrated and %d rated, want 1 and 0", got.Unrated, got.Spans)
	}
}

// GFS publishes no freezing-rain accumulation, so a GFS-sourced pass produces
// no ice values. Inventing one would put a fabricated load on a line nobody
// needs to derate.
func TestAGFSPassProducesNoIce(t *testing.T) {
	t.Parallel()

	fields := icyWeather()
	delete(fields, field.FreezingRain)

	w := newWorld(t).withWeather(fields)
	w.weather.cycle.Model = forecast.ModelGFS

	e := w.engine(t)

	if _, err := e.Compute(
		t.Context(), hazard.ComputeRequest{CycleID: w.weather.cycle.ID},
	); err != nil {
		t.Fatalf("Compute: %v", err)
	}

	for _, quantity := range []hzstore.Quantity{
		hzstore.QuantityIceThickness, hzstore.QuantityIceLoad,
	} {
		values, err := e.SpanValues(t.Context(), w.span,
			[]hzstore.Quantity{quantity}, issuance(),
			issuance(), issuance().Add(6*time.Hour))
		if err != nil {
			t.Fatalf("SpanValues(%s): %v", quantity, err)
		}

		if len(values) != 0 {
			t.Errorf("a GFS pass produced %d %s values", len(values), quantity)
		}
	}

	// The wind and the rating still come out. Losing ice must not lose the
	// quantities that do not depend on it.
	for _, quantity := range []hzstore.Quantity{
		hzstore.QuantityWindLoad, hzstore.QuantityAmpacity,
	} {
		values, err := e.SpanValues(t.Context(), w.span,
			[]hzstore.Quantity{quantity}, issuance(),
			issuance(), issuance().Add(6*time.Hour))
		if err != nil {
			t.Fatalf("SpanValues(%s): %v", quantity, err)
		}

		if len(values) != 3 {
			t.Errorf("%s has %d values in a GFS pass, want 3", quantity, len(values))
		}
	}
}

// The provenance run names the model version and the weather's dataset
// versions. This is the link that turns "what was this rating conditioned on"
// into a query.
func TestThePassRecordsItsProvenance(t *testing.T) {
	t.Parallel()

	w := newWorld(t).withWeather(icyWeather())

	got, err := w.engine(t).Compute(
		t.Context(), hazard.ComputeRequest{CycleID: w.weather.cycle.ID})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	if got.RunID.IsZero() {
		t.Fatal("the pass recorded no run")
	}

	if w.recorder.model.Name != hazard.ModelName ||
		w.recorder.model.Version != hazard.ModelVersion {
		t.Errorf("recorded model %+v, want %s %s",
			w.recorder.model, hazard.ModelName, hazard.ModelVersion)
	}

	if w.recorder.run.ModelVersionID != w.recorder.model.ID {
		t.Errorf("the run names model %s, not the one recorded (%s)",
			w.recorder.run.ModelVersionID, w.recorder.model.ID)
	}

	if len(w.recorder.inputs) != 1 {
		t.Fatalf("the run has %d inputs, want the one dataset version behind "+
			"three grids", len(w.recorder.inputs))
	}

	if w.recorder.inputs[0].DatasetVersionID != w.datasetVersion {
		t.Errorf("the run names dataset %s, want %s",
			w.recorder.inputs[0].DatasetVersionID, w.datasetVersion)
	}

	// And the computation row ties the two ends together.
	stored, err := w.engine(t).Computation(t.Context(), got.ComputationID)
	if err != nil {
		t.Fatalf("Computation: %v", err)
	}

	if stored.RunID != got.RunID || stored.CycleID != w.weather.cycle.ID {
		t.Errorf("the computation row is %+v", stored)
	}
}

// A deployment without a recorder still computes and still stores. What it
// loses is the run, and the row records the absence rather than inventing one.
func TestAPassWithoutARecorderStillComputes(t *testing.T) {
	t.Parallel()

	w := newWorld(t).withWeather(icyWeather())

	e, err := hazard.NewEngine(w.store, nil,
		hazard.WithCompute(w.weather, w.geometry, nil),
		hazard.WithClock(issuance))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	got, err := e.Compute(t.Context(), hazard.ComputeRequest{CycleID: w.weather.cycle.ID})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	if got.Values == 0 {
		t.Error("a pass without a recorder wrote nothing")
	}

	if !got.RunID.IsZero() {
		t.Errorf("a pass without a recorder invented run %s", got.RunID)
	}
}

// A moment with no temperature produces nothing rather than part of the set. A
// span with a wind load and no rating would be a gap nothing downstream could
// reconstruct the reason for.
func TestAMomentWithoutTemperatureProducesNothing(t *testing.T) {
	t.Parallel()

	fields := icyWeather()
	delete(fields, field.Temp2m)

	w := newWorld(t).withWeather(fields)

	got, err := w.engine(t).Compute(
		t.Context(), hazard.ComputeRequest{CycleID: w.weather.cycle.ID})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	if got.Values != 0 {
		t.Errorf("wrote %d values with no temperature", got.Values)
	}

	if got.Covered != 0 {
		t.Errorf("counted %d covered moments with no temperature", got.Covered)
	}
}

// A span outside the model's domain has no samples at all, which is coverage
// information rather than a failure — HRRR is CONUS-only and this is what a
// span in Alberta looks like.
func TestASpanOutsideTheDomainIsUncovered(t *testing.T) {
	t.Parallel()

	w := newWorld(t)

	got, err := w.engine(t).Compute(
		t.Context(), hazard.ComputeRequest{CycleID: w.weather.cycle.ID})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	if got.Values != 0 || got.Spans != 0 {
		t.Errorf("a span with no weather produced %d values over %d spans",
			got.Values, got.Spans)
	}

	if got.Unrated != 0 {
		t.Errorf("a span with no weather was counted unrated; it has a conductor")
	}

	if got.Covered != 0 || got.Total == 0 {
		t.Errorf("coverage was %d of %d, want none of something",
			got.Covered, got.Total)
	}
}

// Ratings vary hour to hour, because the weather does. A pass that wrote the
// same number every hour would have lost the valid time somewhere.
func TestRatingsVaryWithTheHour(t *testing.T) {
	t.Parallel()

	w := newWorld(t)

	samples := make([]forecast.Sample, 0, 12)

	for hour := range 3 {
		at := issuance().Add(time.Duration(hour) * time.Hour)

		for got, value := range map[field.Field]float64{
			field.Temp2m:   295 + float64(hour)*4,
			field.Gust10m:  10,
			field.WindU10m: 5,
			field.WindV10m: 0,
		} {
			samples = append(samples, forecast.Sample{
				SpanID: w.span, Field: got, IssuedAt: issuance(),
				ValidAt: at, Value: value,
			})
		}
	}

	w.weather.samples[w.span] = samples

	e := w.engine(t)

	if _, err := e.Compute(
		t.Context(), hazard.ComputeRequest{CycleID: w.weather.cycle.ID},
	); err != nil {
		t.Fatalf("Compute: %v", err)
	}

	values, err := e.SpanValues(t.Context(), w.span,
		[]hzstore.Quantity{hzstore.QuantityAmpacity}, issuance(),
		issuance(), issuance().Add(6*time.Hour))
	if err != nil {
		t.Fatalf("SpanValues: %v", err)
	}

	if len(values) != 3 {
		t.Fatalf("read %d ratings, want 3", len(values))
	}

	// Hotter each hour, so lower each hour.
	for index := 1; index < len(values); index++ {
		if values[index].Value >= values[index-1].Value {
			t.Errorf("hour %d rated %.1f A, no lower than the %.1f A before it "+
				"despite four degrees more heat",
				index, values[index].Value, values[index-1].Value)
		}
	}
}

// An engine built for reads refuses to compute rather than silently doing
// nothing.
func TestAReadOnlyEngineRefusesToCompute(t *testing.T) {
	t.Parallel()

	e, err := hazard.NewEngine(memory.New(), nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	if _, err := e.Compute(
		t.Context(), hazard.ComputeRequest{CycleID: id.MustNew("wxcyc")},
	); err == nil {
		t.Fatal("a read-only engine computed")
	}
}

// Reading values without an issuance is refused. A window this method chose
// would give the same request a different answer tomorrow.
func TestReadingValuesNeedsAnIssuance(t *testing.T) {
	t.Parallel()

	e, err := hazard.NewEngine(memory.New(), nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	_, err = e.SpanValues(t.Context(), id.MustNew(gid.Span), nil,
		time.Time{}, issuance(), issuance().Add(time.Hour))
	if err == nil {
		t.Fatal("a read with no issuance was accepted")
	}

	_, err = e.SpanValues(t.Context(), id.Nil, nil,
		issuance(), issuance(), issuance().Add(time.Hour))
	if err == nil {
		t.Fatal("a read with no span was accepted")
	}
}

// A route the geometry does not know fails the pass rather than being counted
// as an unrated span. One is a broken reference and the other a gap in the
// asset record, and treating the first as the second would hide it.
func TestAMissingRouteFailsThePass(t *testing.T) {
	t.Parallel()

	w := newWorld(t).withWeather(icyWeather())
	routes := []id.ID{w.route}
	w.geometry.routes = nil

	_, err := w.engine(t).Compute(
		t.Context(), hazard.ComputeRequest{CycleID: w.weather.cycle.ID, Routes: routes})
	if err == nil {
		t.Fatal("a pass over a span whose route is missing succeeded")
	}

	if !errors.Is(err, bserr.ErrNotFound) {
		t.Errorf("error = %v, want it to wrap bserr.ErrNotFound", err)
	}
}
