package mechanical_test

import (
	"math"
	"testing"

	"github.com/xraph/blackstart/extensions/hazard/mechanical"
)

// Wind along a span exerts no crosswind. This is the check that catches a
// sine and cosine swapped — the swapped version still produces a plausible
// force, just wrong for every span whose bearing is not the one tested.
func TestWindParallelToASpanHasNoCrosswind(t *testing.T) {
	t.Parallel()

	// A due-north span, and wind blowing from the south — along it.
	got, err := mechanical.Wind(mechanical.WindInput{
		GustMS: 20, WindUMS: 0, WindVMS: 10,
		TempK: 293, SpanAzimuthDeg: 0, DiameterM: 0.028,
	})
	if err != nil {
		t.Fatalf("Wind: %v", err)
	}

	if math.Abs(got.CrosswindMS) > 1e-9 {
		t.Errorf("crosswind = %v on a parallel wind, want 0", got.CrosswindMS)
	}

	if math.Abs(got.DragNPerM) > 1e-9 {
		t.Errorf("drag = %v with no crosswind", got.DragNPerM)
	}
}

// Wind across a span exerts all of it.
func TestWindPerpendicularToASpanIsFullyCrosswind(t *testing.T) {
	t.Parallel()

	// A due-north span, wind blowing from the west.
	got, err := mechanical.Wind(mechanical.WindInput{
		GustMS: 20, WindUMS: 10, WindVMS: 0,
		TempK: 293, SpanAzimuthDeg: 0, DiameterM: 0.028,
	})
	if err != nil {
		t.Fatalf("Wind: %v", err)
	}

	if math.Abs(got.CrosswindMS-20) > 1e-9 {
		t.Errorf("crosswind = %v on a perpendicular wind, want the full 20", got.CrosswindMS)
	}

	if math.Abs(got.AngleDeg-90) > 1e-9 {
		t.Errorf("angle = %v on a perpendicular wind, want 90", got.AngleDeg)
	}
}

// And a 45-degree wind exerts the component, not all of it and not none.
func TestWindAtFortyFiveDegreesGivesTheComponent(t *testing.T) {
	t.Parallel()

	got, err := mechanical.Wind(mechanical.WindInput{
		GustMS: 20, WindUMS: 10, WindVMS: 10,
		TempK: 293, SpanAzimuthDeg: 0, DiameterM: 0.028,
	})
	if err != nil {
		t.Fatalf("Wind: %v", err)
	}

	want := 20 * math.Sqrt2 / 2

	if math.Abs(got.CrosswindMS-want) > 1e-6 {
		t.Errorf("crosswind = %v, want %v", got.CrosswindMS, want)
	}

	if math.Abs(got.AngleDeg-45) > 1e-6 {
		t.Errorf("angle = %v, want 45", got.AngleDeg)
	}
}

// A span's bearing is a line, not an arrow. A wind hitting a north-south span
// crosswise does the same thing whether the span was surveyed north-to-south
// or south-to-north, and a result that flipped with the survey direction would
// be wrong for half the network.
func TestSpanDirectionDoesNotMatter(t *testing.T) {
	t.Parallel()

	north, err := mechanical.Wind(mechanical.WindInput{
		GustMS: 20, WindUMS: 7, WindVMS: 3,
		TempK: 293, SpanAzimuthDeg: 30, DiameterM: 0.028,
	})
	if err != nil {
		t.Fatalf("Wind: %v", err)
	}

	south, err := mechanical.Wind(mechanical.WindInput{
		GustMS: 20, WindUMS: 7, WindVMS: 3,
		TempK: 293, SpanAzimuthDeg: 210, DiameterM: 0.028,
	})
	if err != nil {
		t.Fatalf("Wind: %v", err)
	}

	if math.Abs(north.DragNPerM-south.DragNPerM) > 1e-9 {
		t.Errorf("reversing the span changed drag: %v then %v",
			north.DragNPerM, south.DragNPerM)
	}
}

// Reversing the wind is the same test from the other side: a westerly and an
// easterly load a north-south span identically.
func TestWindDirectionIsSymmetric(t *testing.T) {
	t.Parallel()

	west, err := mechanical.Wind(mechanical.WindInput{
		GustMS: 20, WindUMS: 10, WindVMS: 4,
		TempK: 293, SpanAzimuthDeg: 0, DiameterM: 0.028,
	})
	if err != nil {
		t.Fatalf("Wind: %v", err)
	}

	east, err := mechanical.Wind(mechanical.WindInput{
		GustMS: 20, WindUMS: -10, WindVMS: -4,
		TempK: 293, SpanAzimuthDeg: 0, DiameterM: 0.028,
	})
	if err != nil {
		t.Fatalf("Wind: %v", err)
	}

	if math.Abs(west.DragNPerM-east.DragNPerM) > 1e-9 {
		t.Errorf("reversing the wind changed drag: %v then %v",
			west.DragNPerM, east.DragNPerM)
	}
}

// Drag scales with the square of the crosswind. Doubling the wind quadruples
// the load, which is why a gust rather than a mean is the input.
func TestDragScalesWithTheSquareOfCrosswind(t *testing.T) {
	t.Parallel()

	base := mechanical.WindInput{
		GustMS: 10, WindUMS: 10, WindVMS: 0,
		TempK: 293, SpanAzimuthDeg: 0, DiameterM: 0.028,
	}

	low, err := mechanical.Wind(base)
	if err != nil {
		t.Fatalf("Wind: %v", err)
	}

	base.GustMS = 20

	high, err := mechanical.Wind(base)
	if err != nil {
		t.Fatalf("Wind: %v", err)
	}

	if ratio := high.DragNPerM / low.DragNPerM; math.Abs(ratio-4) > 1e-6 {
		t.Errorf("doubling the wind changed drag by %v, want 4", ratio)
	}
}

// Ice widens the face the wind pushes on, which is why accretion runs first.
func TestIceIncreasesTheDragArea(t *testing.T) {
	t.Parallel()

	bare := mechanical.WindInput{
		GustMS: 20, WindUMS: 10, WindVMS: 0,
		TempK: 293, SpanAzimuthDeg: 0, DiameterM: 0.028,
	}

	iced := bare
	iced.IceThicknessM = 0.012

	before, err := mechanical.Wind(bare)
	if err != nil {
		t.Fatalf("Wind: %v", err)
	}

	after, err := mechanical.Wind(iced)
	if err != nil {
		t.Fatalf("Wind: %v", err)
	}

	if after.DragNPerM <= before.DragNPerM {
		t.Errorf("icing did not increase drag: %v then %v",
			before.DragNPerM, after.DragNPerM)
	}

	// The face grows by twice the radial thickness, so the load grows in the
	// same proportion. A version that added the thickness once would still
	// pass the inequality above.
	want := (0.028 + 2*0.012) / 0.028

	if ratio := after.DragNPerM / before.DragNPerM; math.Abs(ratio-want) > 1e-9 {
		t.Errorf("icing changed drag by %v, want %v", ratio, want)
	}
}

// Cold air is denser and pushes harder at the same speed. Small, but it is the
// only thing temperature does in this model, so a dropped term would be silent.
func TestColdAirPushesHarder(t *testing.T) {
	t.Parallel()

	warm := mechanical.WindInput{
		GustMS: 20, WindUMS: 10, WindVMS: 0,
		TempK: 303, SpanAzimuthDeg: 0, DiameterM: 0.028,
	}

	cold := warm
	cold.TempK = 253

	hot, err := mechanical.Wind(warm)
	if err != nil {
		t.Fatalf("Wind: %v", err)
	}

	freezing, err := mechanical.Wind(cold)
	if err != nil {
		t.Fatalf("Wind: %v", err)
	}

	if freezing.DragNPerM <= hot.DragNPerM {
		t.Errorf("cold air did not push harder: %v at 253 K, %v at 303 K",
			freezing.DragNPerM, hot.DragNPerM)
	}
}

// A worked number, so that a refactor that leaves every ratio above intact but
// scales the whole result cannot pass. Half of 1.225 kg/m3 times a drag
// coefficient of one, times a 0.028 m face, times 400 m2/s2.
func TestDragMatchesTheHandCalculation(t *testing.T) {
	t.Parallel()

	got, err := mechanical.Wind(mechanical.WindInput{
		GustMS: 20, WindUMS: 10, WindVMS: 0,
		TempK: 288.15, SpanAzimuthDeg: 0, DiameterM: 0.028,
	})
	if err != nil {
		t.Fatalf("Wind: %v", err)
	}

	density := 1.293 * 273.15 / 288.15
	want := 0.5 * density * 1.0 * 0.028 * 20 * 20

	if math.Abs(got.DragNPerM-want) > 1e-9 {
		t.Errorf("drag = %v N/m, want %v", got.DragNPerM, want)
	}

	// Roughly 7 N/m — a few newtons per meter on a bare conductor in a gale.
	// If this ever reads 0.007 or 7000, a unit has gone astray.
	if got.DragNPerM < 5 || got.DragNPerM > 10 {
		t.Errorf("drag = %v N/m, which is not the order of magnitude this "+
			"loading has", got.DragNPerM)
	}
}

// A gust with no prevailing flow has no direction to resolve, and the
// conservative reading is that all of it crosses the span.
func TestCalmMeanWindTakesTheGustAsPerpendicular(t *testing.T) {
	t.Parallel()

	got, err := mechanical.Wind(mechanical.WindInput{
		GustMS: 20, WindUMS: 0, WindVMS: 0,
		TempK: 293, SpanAzimuthDeg: 137, DiameterM: 0.028,
	})
	if err != nil {
		t.Fatalf("Wind: %v", err)
	}

	if math.Abs(got.CrosswindMS-20) > 1e-9 {
		t.Errorf("crosswind = %v with no wind vector, want the full 20", got.CrosswindMS)
	}
}

func TestWindRefusesImplausibleInput(t *testing.T) {
	t.Parallel()

	for name, in := range map[string]mechanical.WindInput{
		"no diameter":   {GustMS: 20, TempK: 293},
		"negative gust": {GustMS: -1, TempK: 293, DiameterM: 0.028},
		"absolute zero": {GustMS: 20, TempK: 0, DiameterM: 0.028},
		"negative ice":  {GustMS: 20, TempK: 293, DiameterM: 0.028, IceThicknessM: -0.01},
	} {
		if _, err := mechanical.Wind(in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
