package scenario_test

import (
	"testing"
	"time"

	"github.com/xraph/blackstart/extensions/scenario/scenario"
)

func validManifest() scenario.Manifest {
	return scenario.Manifest{
		ModelHash:    "sha256:abc",
		WeatherCycle: "wxcyc_01j9xyz0000000000000000000",
		// A time.Time, not a string: hazard's SpanValue carries ValidAt and
		// IssuedAt as time.Time, and this value is compared against them. A
		// string here would make every consumer parse it, which is three
		// chances to disagree about the layout.
		ValidAt: time.Date(2026, 8, 4, 18, 0, 0, 0, time.UTC),
		Policy:  scenario.PolicyAll,
		Seed:    42,
	}
}

// A manifest that cannot name its model cannot be reproduced from.
func TestAManifestNeedsAModelHash(t *testing.T) {
	t.Parallel()

	m := validManifest()
	m.ModelHash = ""

	if err := m.Validate(); err == nil {
		t.Fatal("want an error naming the model hash, got nil")
	}
}

// A cycle spans a whole forecast horizon, so naming it does not determine a
// result. Without a valid time the manifest fails its own invariant.
func TestAManifestNeedsAValidTime(t *testing.T) {
	t.Parallel()

	m := validManifest()
	m.ValidAt = time.Time{}

	if err := m.Validate(); err == nil {
		t.Fatal("want an error naming the valid time, got nil")
	}
}

// The explicit policy is meaningless without a list, and an empty list would
// silently become "screen everything" — the opposite of what was asked.
func TestAnExplicitPolicyNeedsBranches(t *testing.T) {
	t.Parallel()

	m := validManifest()
	m.Policy = scenario.PolicyExplicit

	if err := m.Validate(); err == nil {
		t.Fatal("want an error naming the empty branch list, got nil")
	}
}

func TestAValidManifestPasses(t *testing.T) {
	t.Parallel()

	if err := validManifest().Validate(); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}
