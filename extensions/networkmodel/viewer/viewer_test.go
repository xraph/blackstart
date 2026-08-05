package viewer_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/xraph/blackstart/extensions/networkmodel/canonical"
	"github.com/xraph/blackstart/extensions/networkmodel/viewer"
)

// A network at two voltage levels with one bus that states neither, which is
// the only shape that can tell a per-bus reading from a model-wide one.
func mixedVoltage() canonical.Model {
	return canonical.Model{
		SBaseVA: 100e6,
		VBaseV:  345e3,
		Buses: []canonical.Bus{
			{ID: "bus-1", Kind: 3, VMagnitude: 1, VNominalV: 345e3},
			{ID: "bus-2", Kind: 1, VMagnitude: 1, VNominalV: 138e3},
			{ID: "bus-3", Kind: 1, VMagnitude: 1},
		},
		Branches: []canonical.Branch{
			{ID: "line-1", FromID: "bus-1", ToID: "bus-2", X: 0.1, Tap: 1, InService: true},
		},
	}
}

// The renderer colors by voltage level, so every bus reporting the model base
// drew a 138 kV substation as though it were switchyard at 345 kV. Each bus
// reports its own.
func TestEachBusReportsItsOwnNominalVoltage(t *testing.T) {
	got, err := viewer.Build("m", "h", mixedVoltage(), nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	want := []float64{345, 138, 345}

	for position, kv := range want {
		if got.Buses.NominalKV[position] != kv {
			t.Errorf("%s is drawn at %v kV, want %v",
				got.Buses.IDs[position], got.Buses.NominalKV[position], kv)
		}
	}
}

// The third bus states nothing, and the model base is what it falls back to.
// Zero would be a voltage level of its own as far as a color scale is
// concerned — a bus rendered as though it sat at no voltage at all.
func TestABusThatStatesNoVoltageFallsBackToTheModelBase(t *testing.T) {
	model := mixedVoltage()
	model.VBaseV = 230e3

	got, err := viewer.Build("m", "h", model, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if got.Buses.NominalKV[2] != 230 {
		t.Errorf("bus-3 is drawn at %v kV, want the model base's 230",
			got.Buses.NominalKV[2])
	}
}

// The payload is marshaled by encoding/json, which returns an error for an
// infinity rather than emitting one — so a single bus holding +Inf would fail
// the whole viewer endpoint, with a message naming JSON rather than voltage,
// about a bus nobody was looking at. It falls back instead.
func TestAnUnusableNominalVoltageIsNotDrawn(t *testing.T) {
	for name, volts := range map[string]float64{
		"positive infinity": math.Inf(1),
		"negative infinity": math.Inf(-1),
		"negative":          -138e3,
		"not a number":      math.NaN(),
	} {
		t.Run(name, func(t *testing.T) {
			model := mixedVoltage()
			model.Buses[1].VNominalV = volts

			got, err := viewer.Build("m", "h", model, nil)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}

			// The model base, because the bus states nothing usable.
			if kv := got.Buses.NominalKV[1]; kv != 345 {
				t.Errorf("bus-2 is drawn at %v kV, want the base's 345", kv)
			}

			// And the whole payload still marshals, which is the failure this
			// prevents rather than the value it corrects.
			if _, err := json.Marshal(got); err != nil {
				t.Errorf("the payload no longer marshals: %v", err)
			}
		})
	}
}
