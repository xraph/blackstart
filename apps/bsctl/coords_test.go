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

// A CSV that names every one of the model's buses, and nothing else, leaves
// neither count.
func TestPlacementCountsOfAPerfectMatchIsZeroEverything(t *testing.T) {
	t.Parallel()

	modelID := nmid.NewModel()
	placements := placementsOf(viewer.Placement{
		"bus-1": {Lon: -101.4, Lat: 34.2},
		"bus-2": {Lon: -99.1, Lat: 32.0},
	}, modelID)

	unplaced, unknown := placementCounts(placements, map[string]bool{"bus-1": true, "bus-2": true})

	if unplaced != 0 || unknown != 0 {
		t.Errorf("unplaced=%d unknown=%d, want 0 and 0 for a perfect match", unplaced, unknown)
	}
}

// THE case this function exists for: a CSV in the ACTIVSg distribution's raw
// bus-number convention rather than the canonical bus-<n> one. Every row is
// unknown to the model, and every model bus stays unplaced — not the "placed
// N buses" a caller counting only len(placements) would report.
func TestPlacementCountsOfTheWrongConventionPlacesNothing(t *testing.T) {
	t.Parallel()

	modelID := nmid.NewModel()
	placements := placementsOf(viewer.Placement{
		"1": {Lon: -101.4, Lat: 34.2},
		"2": {Lon: -99.1, Lat: 32.0},
	}, modelID)

	unplaced, unknown := placementCounts(placements, map[string]bool{"bus-1": true, "bus-2": true})

	if unplaced != 2 {
		t.Errorf("unplaced = %d, want 2; the raw-number CSV names none of the model's buses", unplaced)
	}

	if unknown != 2 {
		t.Errorf("unknown = %d, want 2; every row names a bus the model does not have", unknown)
	}
}

// A CSV that is a genuine subset — some of the model's buses, correctly
// named, and nothing extra — reports the gap without inventing a mismatch.
func TestPlacementCountsOfAPartialCSVReportsWhatIsMissing(t *testing.T) {
	t.Parallel()

	modelID := nmid.NewModel()
	placements := placementsOf(viewer.Placement{
		"bus-1": {Lon: -101.4, Lat: 34.2},
	}, modelID)

	unplaced, unknown := placementCounts(
		placements, map[string]bool{"bus-1": true, "bus-2": true, "bus-3": true})

	if unplaced != 2 {
		t.Errorf("unplaced = %d, want 2 (bus-2 and bus-3)", unplaced)
	}

	if unknown != 0 {
		t.Errorf("unknown = %d, want 0; every placed row named a real bus", unknown)
	}
}
