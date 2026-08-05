package risk_test

import (
	"math"
	"testing"
	"time"

	geoasset "github.com/xraph/blackstart/extensions/geo/asset"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	hzstore "github.com/xraph/blackstart/extensions/hazard/store"
	"github.com/xraph/blackstart/extensions/risk"
	"github.com/xraph/blackstart/extensions/risk/fragility"
	rstore "github.com/xraph/blackstart/extensions/risk/store"
	"github.com/xraph/blackstart/extensions/risk/store/memory"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	"github.com/xraph/blackstart/pkg/id"
)

func TestACleanPassWritesBothQuantities(t *testing.T) {
	w := newWorld(t).withHazard(icyLoading())

	got, err := w.engine(t).Compute(t.Context(), risk.ComputeRequest{CycleID: w.cycle})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	if got.Spans != 1 {
		t.Errorf("wrote conductor probabilities for %d spans, want 1", got.Spans)
	}

	if got.Structures != 2 {
		t.Errorf("wrote structure probabilities for %d structures, want 2", got.Structures)
	}

	if got.Scalar != risk.ScalarUncalibrated {
		t.Errorf("scalar %v, want the uncalibrated %v", got.Scalar, risk.ScalarUncalibrated)
	}
}

func TestAnUnclassedStructureIsCountedNotZeroed(t *testing.T) {
	// A structure with no curve and a structure with probability zero are
	// different claims. An ArcGIS-sourced route has no structures at all; an
	// OSM one has about 2% whose kind nobody recorded. Neither is "this
	// structure will not fail".
	w := newWorld(t).withHazard(icyLoading())
	w.geometry.structures[0].Kind = "H-frame"

	got, err := w.engine(t).Compute(t.Context(), risk.ComputeRequest{CycleID: w.cycle})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	if got.Unclassed != 1 || got.Structures != 1 {
		t.Fatalf("counted %d unclassed and %d structures, want 1 and 1",
			got.Unclassed, got.Structures)
	}

	// And it wrote nothing for it, rather than a zero.
	values, _ := w.store.Values().ForComponent(t.Context(),
		w.geometry.structures[0].ID,
		[]rstore.Quantity{rstore.QuantityStructureFailure},
		w.issuedAt, w.issuedAt, w.issuedAt.Add(6*time.Hour))

	if len(values) != 0 {
		t.Errorf("an unclassed structure produced %d values", len(values))
	}
}

func TestASpanWithNoHazardIsCountedSeparately(t *testing.T) {
	// Distinct from Unclassed for the reason hazard keeps its own outcomes
	// apart: one is a gap in the weather coverage and the other a gap in the
	// asset record, and only one of them is anybody's to fix.
	w := newWorld(t) // no hazard values at all

	got, err := w.engine(t).Compute(t.Context(), risk.ComputeRequest{CycleID: w.cycle})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	if got.Unhazarded != 1 || got.Spans != 0 {
		t.Fatalf("counted %d unhazarded and %d spans, want 1 and 0",
			got.Unhazarded, got.Spans)
	}
}

// TestANonFiniteHazardValueIsSkipped covers a NaN row rather than assuming
// fragility's own non-positive guard is enough: fragility.Curve.Probability
// and ConductorProbability both guard intensity <= 0, which NaN is not, so a
// NaN hazard value would otherwise become a stored NaN probability that
// propagates silently through every downstream sum.
func TestANonFiniteHazardValueIsSkipped(t *testing.T) {
	w := newWorld(t)

	corrupt := w.issuedAt
	clean := w.issuedAt.Add(time.Hour)

	w.hazard.values[w.span] = []hzstore.SpanValue{
		{SpanID: w.span, Quantity: hzstore.QuantityWindLoad, IssuedAt: w.issuedAt, ValidAt: corrupt, Value: math.NaN()},
		{SpanID: w.span, Quantity: hzstore.QuantityIceLoad, IssuedAt: w.issuedAt, ValidAt: corrupt, Value: 0},
		{SpanID: w.span, Quantity: hzstore.QuantityWindLoad, IssuedAt: w.issuedAt, ValidAt: clean, Value: 60},
		{SpanID: w.span, Quantity: hzstore.QuantityIceLoad, IssuedAt: w.issuedAt, ValidAt: clean, Value: 0},
	}

	if _, err := w.engine(t).Compute(
		t.Context(), risk.ComputeRequest{CycleID: w.cycle},
	); err != nil {
		t.Fatalf("Compute: %v", err)
	}

	values, err := w.store.Values().ForComponent(t.Context(), w.span,
		[]rstore.Quantity{rstore.QuantityConductorFailure},
		w.issuedAt, w.issuedAt, w.issuedAt.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("ForComponent: %v", err)
	}

	if len(values) != 1 {
		t.Fatalf("stored %d conductor rows, want 1 — the NaN hour skipped, "+
			"the clean one kept", len(values))
	}

	if math.IsNaN(values[0].Probability) || values[0].ValidAt != clean {
		t.Errorf("stored row is %+v, want the clean hour's finite probability", values[0])
	}
}

func TestProbabilityRisesWithTheStorm(t *testing.T) {
	calm := probabilityUnder(t, 5.0)
	storm := probabilityUnder(t, 60.0)

	if !(storm > calm) {
		t.Fatalf("a 60 N/m wind produced P=%v, no worse than 5 N/m at P=%v",
			storm, calm)
	}
}

func TestTheScalarScalesEveryValue(t *testing.T) {
	// Piece 4 fits this. Until then the assertion that matters is that it is
	// applied at all and recorded on the row — a value that cannot say what
	// multiplied it cannot be compared with one computed later.
	full := probabilityWithScalar(t, 1.0)
	half := probabilityWithScalar(t, 0.5)

	if math.Abs(half-full/2) > 1e-9 {
		t.Fatalf("scalar 0.5 gave %v, want half of %v", half, full)
	}
}

// TestTheScalarIsRecordedOnTheRow proves the second half of the scalar
// requirement that TestTheScalarScalesEveryValue does not: not just that the
// multiplication happened, but that the row says what multiplied it. A
// probability computed under an implicit 1.0 and one computed under a fitted
// 0.7 are not comparable unless the stored row can tell them apart.
func TestTheScalarIsRecordedOnTheRow(t *testing.T) {
	const scalar = 0.5

	w := newWorld(t).withHazard(icyLoading())

	e, err := risk.NewEngine(w.store, nil,
		risk.WithCompute(w.hazard, w.weather, w.geometry, w.recorder),
		risk.WithClock(issuance),
		risk.WithScalar(scalar))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	if _, err := e.Compute(t.Context(), risk.ComputeRequest{CycleID: w.cycle}); err != nil {
		t.Fatalf("Compute: %v", err)
	}

	spanValues, err := w.store.Values().ForComponent(t.Context(), w.span,
		[]rstore.Quantity{rstore.QuantityConductorFailure},
		w.issuedAt, w.issuedAt, w.issuedAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("ForComponent(span): %v", err)
	}

	if len(spanValues) == 0 {
		t.Fatal("the pass wrote no conductor probability")
	}

	if spanValues[0].Scalar != scalar {
		t.Errorf("the conductor row carries scalar %v, want %v",
			spanValues[0].Scalar, scalar)
	}

	structureValues, err := w.store.Values().ForComponent(t.Context(),
		w.geometry.structures[0].ID, []rstore.Quantity{rstore.QuantityStructureFailure},
		w.issuedAt, w.issuedAt, w.issuedAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("ForComponent(structure): %v", err)
	}

	if len(structureValues) == 0 {
		t.Fatal("the pass wrote no structure probability")
	}

	if structureValues[0].Scalar != scalar {
		t.Errorf("the structure row carries scalar %v, want %v",
			structureValues[0].Scalar, scalar)
	}
}

// TestScalarOutsideZeroOneIsIgnored proves WithScalar's own bound: a scalar
// this package could not have produced from a bounded curve is rejected
// rather than stored, so a probability written under it can never exceed 1.
func TestScalarOutsideZeroOneIsIgnored(t *testing.T) {
	rejected := probabilityWithScalar(t, 2.0)
	accepted := probabilityWithScalar(t, 1.0)

	if math.Abs(rejected-accepted) > 1e-9 {
		t.Errorf("WithScalar(2.0) gave %v, want it ignored in favor of the "+
			"default %v", rejected, accepted)
	}
}

// TestAStructureBetweenTwoSpansTakesTheHigherLoad exercises the modelling
// decision named in Engine.Compute's doc directly: a structure standing
// between a calm span and a stormy one is rated at the storm's loading, not
// at their average and not at the calm span's.
func TestAStructureBetweenTwoSpansTakesTheHigherLoad(t *testing.T) {
	const (
		calmWindLoadNPerM  = 20.0
		stormWindLoadNPerM = 90.0
	)

	route := id.MustNew(gid.Route)
	calmSpan := id.MustNew(gid.Span)
	stormSpan := id.MustNew(gid.Span)
	middle := id.MustNew(gid.Structure)
	cycleID := id.MustNew("wxcyc")
	issuedAt := issuance()

	h := &fakeHazard{values: map[id.ID][]hzstore.SpanValue{
		calmSpan: {{
			SpanID: calmSpan, Quantity: hzstore.QuantityWindLoad,
			IssuedAt: issuedAt, ValidAt: issuedAt, Value: calmWindLoadNPerM,
		}},
		stormSpan: {{
			SpanID: stormSpan, Quantity: hzstore.QuantityWindLoad,
			IssuedAt: issuedAt, ValidAt: issuedAt, Value: stormWindLoadNPerM,
		}},
	}}

	g := &fakeGeometry{
		routes: []geoasset.Route{{
			ID: route, CarrierID: gid.NewCircuit(), Provenance: geoasset.Surveyed,
		}},
		// The storm span is written first (Sequence 1, processed before the
		// calm span at Sequence 2 in structureLoad's [ordinal, ordinal+1]
		// walk) so that only a real max — not an unconditional last-write —
		// keeps the higher value: a last-wins bug would let the calm span's
		// later write silently overwrite the storm's.
		spans: map[id.ID][]geoasset.Span{
			route: {
				{ID: stormSpan, RouteID: route, Sequence: 1, LengthM: 300},
				{ID: calmSpan, RouteID: route, Sequence: 2, LengthM: 300},
			},
		},
		structures: []geoasset.Structure{
			{ID: id.MustNew(gid.Structure), RouteID: route, Ordinal: 0, Kind: "pole"},
			{ID: middle, RouteID: route, Ordinal: 1, Kind: "pole"},
			{ID: id.MustNew(gid.Structure), RouteID: route, Ordinal: 2, Kind: "pole"},
		},
	}

	w := &fakeWeather{cycle: forecast.Cycle{ID: cycleID, IssuedAt: issuedAt}}
	str := memory.New()

	e, err := risk.NewEngine(str, nil,
		risk.WithCompute(h, w, g, &fakeRecorder{}), risk.WithClock(issuance))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	if _, err := e.Compute(t.Context(), risk.ComputeRequest{CycleID: cycleID}); err != nil {
		t.Fatalf("Compute: %v", err)
	}

	values, err := str.Values().ForComponent(t.Context(), middle,
		[]rstore.Quantity{rstore.QuantityStructureFailure},
		issuedAt, issuedAt, issuedAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("ForComponent: %v", err)
	}

	if len(values) != 1 {
		t.Fatalf("the middle structure wrote %d values, want 1", len(values))
	}

	curve, ok := fragility.ByClass(fragility.Pole)
	if !ok {
		t.Fatal("fragility.ByClass(Pole) has no curve")
	}

	want := curve.Probability(stormWindLoadNPerM)

	if math.Abs(values[0].Probability-want) > 1e-9 {
		t.Errorf("the middle structure's probability is %v, want %v — the "+
			"storm span's loading, not the calm span's or an average of both",
			values[0].Probability, want)
	}
}

// probabilityUnder runs a clean pass with the given wind load, ice held at
// zero, and returns the conductor probability the pass wrote for the span.
func probabilityUnder(t *testing.T, windLoadNPerM float64) float64 {
	t.Helper()

	w := newWorld(t).withHazard(map[hzstore.Quantity]float64{
		hzstore.QuantityWindLoad: windLoadNPerM,
		hzstore.QuantityIceLoad:  0,
	})

	if _, err := w.engine(t).Compute(
		t.Context(), risk.ComputeRequest{CycleID: w.cycle},
	); err != nil {
		t.Fatalf("Compute: %v", err)
	}

	return firstConductorProbability(t, w)
}

// probabilityWithScalar runs a clean pass under a fixed storm with the given
// scalar and returns the conductor probability the pass wrote.
func probabilityWithScalar(t *testing.T, scalar float64) float64 {
	t.Helper()

	const stormWindLoadNPerM = 60.0

	w := newWorld(t).withHazard(map[hzstore.Quantity]float64{
		hzstore.QuantityWindLoad: stormWindLoadNPerM,
		hzstore.QuantityIceLoad:  0,
	})

	e, err := risk.NewEngine(w.store, nil,
		risk.WithCompute(w.hazard, w.weather, w.geometry, w.recorder),
		risk.WithClock(issuance),
		risk.WithScalar(scalar))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	if _, err := e.Compute(t.Context(), risk.ComputeRequest{CycleID: w.cycle}); err != nil {
		t.Fatalf("Compute: %v", err)
	}

	return firstConductorProbability(t, w)
}

// firstConductorProbability reads back the span's conductor probability at
// the first hour of the world's fixture.
func firstConductorProbability(t *testing.T, w *world) float64 {
	t.Helper()

	values, err := w.store.Values().ForComponent(t.Context(), w.span,
		[]rstore.Quantity{rstore.QuantityConductorFailure},
		w.issuedAt, w.issuedAt, w.issuedAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("ForComponent: %v", err)
	}

	if len(values) == 0 {
		t.Fatal("the pass wrote no conductor probability")
	}

	return values[0].Probability
}
