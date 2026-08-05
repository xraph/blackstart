package fragility_test

import (
	"math"
	"testing"

	"github.com/xraph/blackstart/extensions/risk/fragility"
)

func TestACurveIsBoundedAndMonotone(t *testing.T) {
	curve, ok := fragility.ByClass(fragility.Lattice)
	if !ok {
		t.Fatal("no curve for a lattice tower")
	}

	previous := -1.0

	// 14 and 18 N/m sit inside the lattice curve's transition band (median
	// 15.85 N/m): without them, this list is four points saturated at 0,
	// six saturated at 1, and nothing in between, which passes for any step
	// function — including the wrong-by-a-factor-of-2 dispersion this test
	// once let through. See TestACurveStrictlyRisesThroughItsTransitionBand
	// below for the assertion that actually distinguishes a lognormal from a
	// step.
	for _, intensity := range []float64{0, 1, 5, 10, 14, 18, 20, 30, 40, 60, 100, 1000} {
		got := curve.Probability(intensity)

		if got < 0 || got > 1 {
			t.Fatalf("P(%v) = %v, outside [0,1]", intensity, got)
		}

		if got < previous {
			t.Fatalf("P(%v) = %v fell below the previous %v; a fragility curve "+
				"that is not monotone in intensity says a stronger storm is "+
				"safer", intensity, got, previous)
		}

		previous = got
	}
}

// TestACurveStrictlyRisesThroughItsTransitionBand catches a curve so steep
// it behaves like a step function. TestACurveIsBoundedAndMonotone alone
// cannot: a step function is technically monotone too, it just saturates at
// 0 or 1 everywhere outside a razor-thin band, which is exactly what an
// undoubled dispersion produced here (P(10)≈0, P(15.85)=0.5, P(20)≈1). This
// asserts a strict increase across points inside the lattice curve's
// transition band, where a correctly-dispersed lognormal should still be
// visibly rising rather than already saturated.
func TestACurveStrictlyRisesThroughItsTransitionBand(t *testing.T) {
	curve, ok := fragility.ByClass(fragility.Lattice)
	if !ok {
		t.Fatal("no curve for a lattice tower")
	}

	previous := -1.0

	for _, intensity := range []float64{10, 14, 18, 20} {
		got := curve.Probability(intensity)

		if got <= previous {
			t.Fatalf("P(%v) = %v did not strictly exceed the previous %v; "+
				"the curve is not visibly rising through its own transition "+
				"band, which is what a dispersion too small for the "+
				"intensity measure's units looks like", intensity, got, previous)
		}

		previous = got
	}
}

func TestZeroIntensityBreaksNothing(t *testing.T) {
	curve, _ := fragility.ByClass(fragility.Lattice)

	// ln(0) is negative infinity, so this is the input that produces NaN if
	// the guard is missing — and NaN propagates silently through every sum
	// downstream rather than failing where it was made.
	if got := curve.Probability(0); got != 0 {
		t.Fatalf("P(0) = %v, want 0", got)
	}
}

func TestTheMedianIsWhereHalfTheStructuresFail(t *testing.T) {
	curve, _ := fragility.ByClass(fragility.Lattice)

	if got := curve.Probability(curve.Median); math.Abs(got-0.5) > 1e-9 {
		t.Fatalf("P(median) = %v, want 0.5 — that is what a median is", got)
	}
}

func TestEveryClassHasACitedCurve(t *testing.T) {
	// Portal (H-frame) is not in this list: see the [fragility.Class] doc.
	// No open-access or national-laboratory source turned up a numeric
	// median and dispersion for it, so it was removed from the closed set
	// rather than given an invented one — the outcome step 4 of this
	// package's brief calls for when that happens.
	for _, class := range []fragility.Class{
		fragility.Lattice, fragility.Pole,
	} {
		curve, ok := fragility.ByClass(class)
		if !ok {
			t.Fatalf("no curve for %s", class)
		}

		if curve.Source == "" {
			t.Errorf("%s has an uncited curve; section 8 of the project spec "+
				"requires these be traceable to their literature", class)
		}

		if curve.Median <= 0 || curve.Dispersion <= 0 {
			t.Errorf("%s has θ=%v β=%v; both must be positive for the "+
				"lognormal to be defined", class, curve.Median, curve.Dispersion)
		}
	}
}
