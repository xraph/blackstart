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
	// six saturated at 1, and nothing in between. Note that strict
	// monotonicity through those two points is not itself proof the curve
	// has the right steepness — a step function is strictly increasing too,
	// at every one of its steps — so this loop only bounds and orders the
	// curve. See TestACurveIsNotSaturatedInsideItsTransitionBand below for
	// the assertion that actually distinguishes a lognormal of the right
	// dispersion from a step function.
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

// TestACurveIsNotSaturatedInsideItsTransitionBand catches a curve so steep it
// behaves like a step function — which neither the bounded-and-monotone
// check above nor a strict-increase check would catch, since a step function
// is strictly increasing too, at every one of its steps
// (β=0.0397 at {10, 14, 18, 20} → 1.98e-31, 0.00088, 0.9993, ~1 — each value
// strictly exceeds the last, so a "strictly rises" assertion alone passes on
// the wrong, halved dispersion just as readily as the right one; an earlier
// version of this test asserted exactly that and did not catch the bug it
// was written for).
//
// What distinguishes a lognormal of the right steepness from a step is
// whether it is still visibly *in progress* at points inside its own
// transition band, rather than already pinned to an extreme. This asserts
// magnitude floors and ceilings at two such points for the lattice curve
// (median 15.85 N/m): P(14) — one notch below the median — must be small but
// not vanishing, and P(18) — one notch above — must be large but not yet
// saturated. The correct dispersion (0.0794) gives P(14)=0.059 and
// P(18)=0.945, comfortably inside both windows; the halved dispersion
// (0.0397) this test was written to catch gives P(14)=0.00088 and
// P(18)=0.9993, outside both.
func TestACurveIsNotSaturatedInsideItsTransitionBand(t *testing.T) {
	curve, ok := fragility.ByClass(fragility.Lattice)
	if !ok {
		t.Fatal("no curve for a lattice tower")
	}

	if got := curve.Probability(14); got < 0.01 || got > 0.20 {
		t.Fatalf("P(14) = %v, want in [0.01, 0.20]; a value this close to 0 "+
			"means the curve is already saturated below the median instead "+
			"of still rising through it", got)
	}

	if got := curve.Probability(18); got < 0.80 || got > 0.999 {
		t.Fatalf("P(18) = %v, want in [0.80, 0.999]; a value this close to 1 "+
			"means the curve is already saturated above the median instead "+
			"of still rising through it", got)
	}
}

// TestPoleCurveIsNotSaturatedInsideItsTransitionBand is the Pole analog of
// TestACurveIsNotSaturatedInsideItsTransitionBand above. Without it, nothing
// in this package pins Pole's median or its β-doubling — TestACurveIsBoundedAndMonotone
// and the tests below it exercise Lattice only, and halving Pole.Dispersion
// back to 0.2223 (the un-doubled value curve.go's own comment derives, and
// the exact bug this doubling exists to prevent) leaves every other test in
// this file green.
//
// Pole's median is 106.696 N/m and its correct dispersion is 0.4446 — much
// wider than Lattice's 0.0794, so the curve is nowhere near a step function
// even at half that dispersion. The two points below are chosen far enough
// from the median that the correct and halved dispersions land in visibly
// different, non-overlapping windows: P(70) = 0.172 at β=0.4446 versus 0.029
// at β=0.2223; P(150) = 0.778 at β=0.4446 versus 0.937 at β=0.2223.
func TestPoleCurveIsNotSaturatedInsideItsTransitionBand(t *testing.T) {
	curve, ok := fragility.ByClass(fragility.Pole)
	if !ok {
		t.Fatal("no curve for a pole")
	}

	if got := curve.Probability(70); got < 0.10 || got > 0.30 {
		t.Fatalf("P(70) = %v, want in [0.10, 0.30]; a value outside this "+
			"window means Pole's dispersion has drifted from 0.4446 — this "+
			"window was chosen to exclude 0.029, the value the halved, "+
			"un-doubled dispersion (0.2223) produces here", got)
	}

	if got := curve.Probability(150); got < 0.65 || got > 0.90 {
		t.Fatalf("P(150) = %v, want in [0.65, 0.90]; a value outside this "+
			"window means Pole's dispersion has drifted from 0.4446 — this "+
			"window was chosen to exclude 0.937, the value the halved, "+
			"un-doubled dispersion (0.2223) produces here", got)
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
