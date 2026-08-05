package scenario_test

import (
	"encoding/json"
	"reflect"
	"strings"
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

// A policy that is neither declared value is refused by name.
//
// The typo is the dangerous input, not the obviously absurd one: everything
// downstream asks "is this explicit?" and reads anything else as "screen
// every in-service branch". So "explict" with a thirty-branch list passes
// every other check in this method, screens the whole network, and stores a
// manifest describing a run that did not happen. An absent policy is the same
// failure with an emptier spelling — absent means unknown here, not "all".
//
//nolint:misspell // the misspelling of "explicit" is the input under test
func TestAPolicyMustBeOneOfTheTwoDeclaredValues(t *testing.T) {
	t.Parallel()

	for _, policy := range []scenario.Policy{"explict", "", "ALL", "none"} {
		m := validManifest()
		m.Policy = policy
		m.Branches = []string{"br_one", "br_two"}

		err := m.Validate()
		if err == nil {
			t.Errorf("policy %q was accepted; it is read as %q downstream",
				policy, scenario.PolicyAll)

			continue
		}

		// Named, because the whole point is that the author cannot see the
		// difference between their typo and the value they meant.
		if !strings.Contains(err.Error(), string(policy)) {
			t.Errorf("the refusal of %q does not quote it: %v", policy, err)
		}
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

// Cloning a manifest breaks the sharing a plain copy leaves behind.
//
// Manifest is a value type, so every scalar it holds is copied by assignment
// and the compiler makes that look total. Branches is a slice, and a copied
// struct shares its backing array — which is the one way an edit to a
// scenario can still reach a run that was frozen before it.
func TestCloneDoesNotShareTheBranchList(t *testing.T) {
	t.Parallel()

	original := validManifest()
	original.Policy = scenario.PolicyExplicit
	original.Branches = []string{"nmdl_one", "nmdl_two"}

	frozen := original.Clone()

	original.Branches[0] = "nmdl_edited"

	if frozen.Branches[0] != "nmdl_one" {
		t.Errorf("the clone followed the original: got %q, want %q",
			frozen.Branches[0], "nmdl_one")
	}
}

// A nil branch list clones to a nil one rather than to an empty slice, so a
// cloned manifest still serializes to the bytes the original did — the JSON
// keys are pinned, and `[]` where `null` was is a stored manifest that no
// longer round-trips to itself.
func TestCloningAManifestWithoutBranchesChangesNothing(t *testing.T) {
	t.Parallel()

	original := validManifest()

	frozen := original.Clone()

	if frozen.Branches != nil {
		t.Errorf("Branches cloned to %#v, want nil", frozen.Branches)
	}

	if !reflect.DeepEqual(frozen, original) {
		t.Errorf("the clone differs from the original: %+v", frozen)
	}
}
