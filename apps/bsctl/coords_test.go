package bsctl

import (
	"testing"

	nmid "github.com/xraph/blackstart/extensions/networkmodel/id"
	"github.com/xraph/blackstart/extensions/networkmodel/viewer"
)

// The CSV's own shape becomes storable placements, one per bus.
func TestPlacementsOfCarriesEveryBus(t *testing.T) {
	t.Parallel()

	modelID := nmid.NewModel()
	got := placementsOf(viewer.Placement{
		"bus-1": {Lon: -101.4, Lat: 34.2},
		"bus-2": {Lon: -99.1, Lat: 32.0, Elevation: 320},
	}, modelID)

	if len(got) != 2 {
		t.Fatalf("placementsOf returned %d, want 2", len(got))
	}

	for _, placement := range got {
		if placement.ModelID != modelID {
			t.Errorf("placement %s belongs to %s, want %s",
				placement.Bus, placement.ModelID, modelID)
		}

		if err := placement.Validate(); err != nil {
			t.Errorf("placement %s does not validate: %v", placement.Bus, err)
		}
	}
}

// Stable order out of a map, so two runs over one file write the same thing
// and a diff of the result means something.
func TestPlacementsOfIsOrderedByBus(t *testing.T) {
	t.Parallel()

	got := placementsOf(viewer.Placement{
		"bus-2": {Lon: -99.1, Lat: 32.0},
		"bus-1": {Lon: -101.4, Lat: 34.2},
	}, nmid.NewModel())

	if got[0].Bus != "bus-1" {
		t.Errorf("first placement is %s, want bus-1", got[0].Bus)
	}
}
