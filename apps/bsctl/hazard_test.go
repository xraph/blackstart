package bsctl

import (
	"math"
	"testing"

	"github.com/xraph/blackstart/extensions/networkmodel/model"
)

// Volts on the row, kilovolts in the candidate. The single conversion site.
func TestBranchVoltagesConvertToKilovolts(t *testing.T) {
	t.Parallel()

	from := model.Bus{Ordinal: "bus-1", VNominalV: 138000}
	to := model.Bus{Ordinal: "bus-2", VNominalV: 138000}

	gotFrom, gotTo := branchVoltagesKV(from, to)

	if gotFrom != 138 || gotTo != 138 {
		t.Fatalf("got %v/%v kV, want 138/138", gotFrom, gotTo)
	}
}

// A bus stating nothing converts to zero kV, which PlanAssumptions reads as
// "no voltage" — a skip rather than a default.
func TestABusStatingNothingConvertsToZero(t *testing.T) {
	t.Parallel()

	from := model.Bus{Ordinal: "bus-1", VNominalV: 0}
	to := model.Bus{Ordinal: "bus-2", VNominalV: 138000}

	gotFrom, gotTo := branchVoltagesKV(from, to)

	if gotFrom != 0 {
		t.Fatalf("an unstated bus converted to %v kV, want 0", gotFrom)
	}

	if gotTo != 138 {
		t.Fatalf("got %v kV, want 138", gotTo)
	}
}

// An infinite voltage is refused by canonical.StatedNominalV, which is the one
// definition of "states a voltage". Converting one would produce an infinite
// rating rather than a skip.
func TestAnInfiniteVoltageConvertsToZero(t *testing.T) {
	t.Parallel()

	from := model.Bus{Ordinal: "bus-1", VNominalV: math.Inf(1)}
	to := model.Bus{Ordinal: "bus-2", VNominalV: 138000}

	gotFrom, _ := branchVoltagesKV(from, to)

	if gotFrom != 0 {
		t.Fatalf("an infinite voltage converted to %v kV, want 0", gotFrom)
	}
}
