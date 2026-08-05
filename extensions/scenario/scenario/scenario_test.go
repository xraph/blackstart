package scenario_test

import (
	"encoding/json"
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

// The exact wire form, not just a round trip. Every round-trip test in this
// codebase marshals and unmarshals with the same struct — it would pass
// whether the tags read model_hash, modelHash, or nothing at all, because
// json.Unmarshal silently ignores unknown keys. A run's frozen manifest is
// stored as this JSON, so a later rename of one of these tags — say
// weather_cycle to forecast_cycle — would make new writes fine while every
// already-stored run's manifest unmarshals with WeatherCycle == "": no
// error, no signal, and the reproducibility invariant the manifest exists to
// protect fails silently. Pinning the literal is what makes that rename
// something a change has to confront instead of something it can pass
// through unnoticed.
func TestManifestJSONKeysAreExact(t *testing.T) {
	t.Parallel()

	m := scenario.Manifest{
		ModelHash:    "sha256:abc123",
		WeatherCycle: "wxcyc_01j9xyz0000000000000000000",
		ValidAt:      time.Date(2026, 8, 4, 18, 0, 0, 0, time.UTC),
		HazardConfig: "ice-storm-v2",
		Policy:       scenario.PolicyExplicit,
		Branches:     []string{"branch:14", "branch:22"},
		Seed:         42,
	}

	got, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	want := `{"model_hash":"sha256:abc123","weather_cycle":"wxcyc_01j9xyz0000000000000000000",` +
		`"valid_at":"2026-08-04T18:00:00Z","hazard_config":"ice-storm-v2","policy":"explicit",` +
		`"branches":["branch:14","branch:22"],"seed":42}`

	if string(got) != want {
		t.Errorf("Manifest marshaled as:\n%s\nwant:\n%s", got, want)
	}
}
