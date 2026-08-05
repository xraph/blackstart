package memory_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	scnid "github.com/xraph/blackstart/extensions/scenario/id"
	"github.com/xraph/blackstart/extensions/scenario/scenario"
	"github.com/xraph/blackstart/extensions/scenario/store/memory"
	"github.com/xraph/blackstart/pkg/bserr"
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

func aScenario(name string, createdAt time.Time) scenario.Scenario {
	return scenario.Scenario{
		ID:        scnid.NewScenario(),
		Name:      name,
		Manifest:  validManifest(),
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}
}

// Updating a stored scenario replaces it, and the replacement is what a later
// Get returns.
func TestUpdateReplacesAStoredScenario(t *testing.T) {
	t.Parallel()

	s := memory.New()
	ctx := context.Background()

	in := aScenario("August heat wave", time.Now().UTC())

	if _, err := s.Scenarios().Create(ctx, in); err != nil {
		t.Fatalf("Create: %v", err)
	}

	edited := in
	edited.Name = "August derecho"
	edited.Manifest.ModelHash = "sha256:def"

	if _, err := s.Scenarios().Update(ctx, edited); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := s.Scenarios().Get(ctx, in.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Name != edited.Name || got.Manifest.ModelHash != edited.Manifest.ModelHash {
		t.Errorf("the edit did not land: got %q / %q", got.Name, got.Manifest.ModelHash)
	}
}

// Updating a scenario nobody defined is not found, not a quiet insert: an
// Update that created one would turn a mistyped identifier into a second
// scenario nobody meant to define.
func TestUpdateRefusesAScenarioThatDoesNotExist(t *testing.T) {
	t.Parallel()

	s := memory.New()

	_, err := s.Scenarios().Update(context.Background(), aScenario("nowhere", time.Now().UTC()))
	if !errors.Is(err, bserr.ErrNotFound) {
		t.Fatalf("Update of an unknown scenario returned %v, want not-found", err)
	}
}

// Listing returns scenarios most recently created first, and a limit of zero
// returns every one of them rather than some store-chosen default.
func TestListReturnsScenariosMostRecentFirst(t *testing.T) {
	t.Parallel()

	s := memory.New()
	ctx := context.Background()

	base := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)

	var wantOrder []string

	for i := range 3 {
		in := aScenario("scenario", base.Add(time.Duration(i)*time.Hour))

		if _, err := s.Scenarios().Create(ctx, in); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}

		wantOrder = append(wantOrder, in.ID.String())
	}

	got, err := s.Scenarios().List(ctx, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(got) != len(wantOrder) {
		t.Fatalf("got %d scenarios, want %d — a limit of zero means everything", len(got),
			len(wantOrder))
	}

	for i, found := range got {
		want := wantOrder[len(wantOrder)-1-i]
		if found.ID.String() != want {
			t.Errorf("position %d: got %s, want %s (most recent first)", i, found.ID, want)
		}
	}

	limited, err := s.Scenarios().List(ctx, 2)
	if err != nil {
		t.Fatalf("List(2): %v", err)
	}

	if len(limited) != 2 {
		t.Errorf("got %d scenarios for a limit of 2", len(limited))
	}
}
