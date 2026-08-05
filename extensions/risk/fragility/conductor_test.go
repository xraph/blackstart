package fragility_test

import (
	"testing"

	"github.com/xraph/blackstart/extensions/risk/fragility"
)

func TestAConductorProbabilityIsBoundedAndRisesWithLoad(t *testing.T) {
	previous := -1.0

	for _, wind := range []float64{0, 5, 10, 20, 40, 80} {
		got := fragility.ConductorProbability(wind, 0, 300, 1)

		if got < 0 || got > 1 {
			t.Fatalf("P(wind=%v) = %v, outside [0,1]", wind, got)
		}

		if got < previous {
			t.Fatalf("P(wind=%v) = %v fell below %v", wind, got, previous)
		}

		previous = got
	}
}

func TestALongerSpanIsRiskier(t *testing.T) {
	// The whole reason the conductor form is per unit length: a 965 m span has
	// more conductor exposed than a 207 m one, and the survey measured both on
	// real lines.
	short := fragility.ConductorProbability(30, 0, 207, 1)
	long := fragility.ConductorProbability(30, 0, 965, 1)

	if !(long > short) {
		t.Fatalf("965 m span P=%v is not riskier than 207 m P=%v", long, short)
	}
}

func TestAZeroLengthSpanCannotFail(t *testing.T) {
	if got := fragility.ConductorProbability(30, 0, 0, 1); got != 0 {
		t.Fatalf("P(length=0) = %v, want 0", got)
	}
}

func TestIceAddsToTheRate(t *testing.T) {
	dry := fragility.ConductorRate(30, 0)
	icy := fragility.ConductorRate(30, 2)

	if !(icy > dry) {
		t.Fatalf("ice did not raise the rate: dry=%v icy=%v", dry, icy)
	}
}
