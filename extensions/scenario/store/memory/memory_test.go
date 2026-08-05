package memory_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	scnid "github.com/xraph/blackstart/extensions/scenario/id"
	"github.com/xraph/blackstart/extensions/scenario/scenario"
	"github.com/xraph/blackstart/extensions/scenario/store/memory"
)

func validManifest() scenario.Manifest {
	return scenario.Manifest{
		ModelHash:    "sha256:abc",
		WeatherCycle: "wxcyc_01j9xyz0000000000000000000",
		ValidAt:      time.Date(2026, 8, 4, 18, 0, 0, 0, time.UTC),
		Policy:       scenario.PolicyAll,
		Seed:         42,
	}
}

// A stored scenario round-trips: what Get returns is what Create was given,
// manifest included.
func TestAStoredScenarioRoundTrips(t *testing.T) {
	t.Parallel()

	s := memory.New()
	ctx := context.Background()

	in := scenario.Scenario{
		ID:        scnid.NewScenario(),
		Name:      "August heat wave",
		Manifest:  validManifest(),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	if _, err := s.Scenarios().Create(ctx, in); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Scenarios().Get(ctx, in.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Name != in.Name {
		t.Errorf("name did not round-trip: got %q, want %q", got.Name, in.Name)
	}

	// Manifest has a slice field (Branches), so it is compared with
	// DeepEqual rather than ==.
	if !reflect.DeepEqual(got.Manifest, in.Manifest) {
		t.Errorf("manifest did not round-trip: got %+v, want %+v", got.Manifest, in.Manifest)
	}
}
