package model_test

import (
	"errors"
	"testing"

	nmid "github.com/xraph/blackstart/extensions/networkmodel/id"
	"github.com/xraph/blackstart/extensions/networkmodel/model"
	"github.com/xraph/blackstart/pkg/bserr"
)

func aPlacement() model.Placement {
	return model.Placement{
		ID:      nmid.NewPlacement(),
		ModelID: nmid.NewModel(),
		Bus:     "bus-1",
		Lon:     -101.4,
		Lat:     34.2,
	}
}

func TestAValidPlacementValidates(t *testing.T) {
	t.Parallel()

	if err := aPlacement().Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

// Out-of-range coordinates are the failure that renders: a longitude of 200
// draws somewhere, and the map looks like a rendering fault rather than a
// data one.
func TestAPlacementOffTheEarthIsInvalid(t *testing.T) {
	t.Parallel()

	for name, break_ := range map[string]func(*model.Placement){
		"longitude too high": func(p *model.Placement) { p.Lon = 200 },
		"longitude too low":  func(p *model.Placement) { p.Lon = -200 },
		"latitude too high":  func(p *model.Placement) { p.Lat = 91 },
		"latitude too low":   func(p *model.Placement) { p.Lat = -91 },
		"no bus":             func(p *model.Placement) { p.Bus = "" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := aPlacement()
			break_(&got)

			if err := got.Validate(); !errors.Is(err, bserr.ErrInvalid) {
				t.Errorf("Validate() = %v, want ErrInvalid", err)
			}
		})
	}
}

// Null Island is a real position and a common one for a bus whose coordinate
// was never filled in. It is accepted here and made conspicuous by the
// viewer, which already reports how many buses drew at 0,0 — refusing it
// would reject a legitimate coordinate to catch a data-entry habit.
func TestAPlacementAtNullIslandIsValid(t *testing.T) {
	t.Parallel()

	got := aPlacement()
	got.Lon, got.Lat = 0, 0

	if err := got.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}
