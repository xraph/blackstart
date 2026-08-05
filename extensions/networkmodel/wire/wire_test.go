package wire_test

import (
	"math"
	"strings"
	"testing"

	solverv1 "github.com/xraph/blackstart/proto/gen/go/blackstart/solver/v1"

	"github.com/xraph/blackstart/extensions/networkmodel/canonical"
	"github.com/xraph/blackstart/extensions/networkmodel/wire"
)

// A 100 MVA, 138 kV base. Z = 138000² / 100e6 = 190.44 Ω, which is small
// enough to multiply by hand and large enough that dividing instead of
// multiplying is unmistakable.
const (
	sBase = 100e6
	vBase = 138e3
	zBase = vBase * vBase / sBase
)

func twoBus() canonical.Model {
	return canonical.Model{
		SBaseVA: sBase,
		VBaseV:  vBase,
		Buses: []canonical.Bus{
			{
				ID: "bus-1", Kind: 3, VMagnitude: 1.06,
				QMin: math.Inf(-1), QMax: math.Inf(1),
			},
			{
				ID: "bus-2", Kind: 1, PInjection: -0.5, QInjection: -0.2,
				VMagnitude: 1.0, VAngle: -0.05, GShunt: 0.01, BShunt: 0.19,
				QMin: math.Inf(-1), QMax: math.Inf(1),
			},
		},
		Branches: []canonical.Branch{
			{
				ID: "branch-0", FromID: "bus-1", ToID: "bus-2",
				R: 0.01, X: 0.1, B: 0.02, Rating: 2,
				InService: true, Tap: 1,
			},
		},
	}
}

func build(t *testing.T, in canonical.Model) *solverv1.NetworkModel {
	t.Helper()

	got, err := wire.Model("two-bus", in)
	if err != nil {
		t.Fatalf("Model: %v", err)
	}

	return got
}

// Every conversion checked against a figure computed by hand, not against this
// implementation's own output. A wrong base produces a network that solves and
// is wrong by a constant factor, and no downstream check catches that.
func TestPowerAndVoltageConvertAgainstTheStatedBase(t *testing.T) {
	got := build(t, twoBus())

	// −0.5 per unit on a 100 MVA base is −50 MW.
	if p := got.GetBuses()[1].GetPInjectionW(); math.Abs(p-(-50e6)) > 1 {
		t.Errorf("injection is %v W, want -50 MW", p)
	}

	if q := got.GetBuses()[1].GetQInjectionVar(); math.Abs(q-(-20e6)) > 1 {
		t.Errorf("reactive injection is %v VAr, want -20 MVAr", q)
	}

	// 1.06 per unit on a 138 kV base is 146.28 kV.
	if v := got.GetBuses()[0].GetVMagnitudeV(); math.Abs(v-146.28e3) > 1 {
		t.Errorf("voltage is %v V, want 146.28 kV", v)
	}

	// Radians on both sides. Converting again would be a factor of 57 that
	// still solves.
	if a := got.GetBuses()[1].GetVAngleRad(); math.Abs(a-(-0.05)) > 1e-12 {
		t.Errorf("angle is %v, want -0.05 radians unchanged", a)
	}
}

// The single most consequential line in the package. 0.1 per unit on a
// 190.44 Ω base is 19.044 Ω.
func TestImpedanceMultipliesByTheBaseImpedance(t *testing.T) {
	got := build(t, twoBus())

	if x := got.GetBranches()[0].GetXOhm(); math.Abs(x-0.1*zBase) > 1e-9 {
		t.Errorf("reactance is %v Ω, want %v", x, 0.1*zBase)
	}

	if r := got.GetBranches()[0].GetROhm(); math.Abs(r-0.01*zBase) > 1e-9 {
		t.Errorf("resistance is %v Ω, want %v", r, 0.01*zBase)
	}
}

// An admittance is the reciprocal of an impedance, so it divides. Multiplying
// would be wrong by the base impedance squared — about 10^8 here, which is
// conspicuous, and about 1 on a unit-base network, which is not. A test built
// on a unit base would pass on the wrong implementation.
func TestAdmittanceDividesByTheBaseImpedance(t *testing.T) {
	got := build(t, twoBus())

	if b := got.GetBuses()[1].GetBShuntS(); math.Abs(b-0.19/zBase) > 1e-15 {
		t.Errorf("susceptance is %v S, want %v", b, 0.19/zBase)
	}

	if g := got.GetBuses()[1].GetGShuntS(); math.Abs(g-0.01/zBase) > 1e-15 {
		t.Errorf("conductance is %v S, want %v", g, 0.01/zBase)
	}

	if b := got.GetBranches()[0].GetBShuntS(); math.Abs(b-0.02/zBase) > 1e-15 {
		t.Errorf("line charging is %v S, want %v", b, 0.02/zBase)
	}

	// And it is genuinely smaller than one, which multiplying would not be.
	if got.GetBuses()[1].GetBShuntS() > 1 {
		t.Error("a per-unit susceptance converted to more than one siemens")
	}
}

// Absent means unlimited, which is not zero. Sending an infinity as a finite
// number would pin a generator that has no stated limit.
func TestUnlimitedReactivePowerIsAbsentNotZero(t *testing.T) {
	got := build(t, twoBus())

	if got.GetBuses()[0].QMinVar != nil || got.GetBuses()[0].QMaxVar != nil {
		t.Errorf("an unlimited bus carries limits %v to %v",
			got.GetBuses()[0].GetQMinVar(), got.GetBuses()[0].GetQMaxVar())
	}

	// A stated limit does travel, and on the power base.
	limited := twoBus()
	limited.Buses[0].QMin, limited.Buses[0].QMax = -0.4, 0.5

	converted := build(t, limited)

	if converted.GetBuses()[0].QMinVar == nil || converted.GetBuses()[0].QMaxVar == nil {
		t.Fatal("stated limits were dropped")
	}

	if q := converted.GetBuses()[0].GetQMaxVar(); math.Abs(q-50e6) > 1 {
		t.Errorf("upper limit is %v VAr, want 50 MVAr", q)
	}
}

// Branch endpoints become indices into the bus list, and the order is the
// canonical model's. A swapped index sends a network that solves and describes
// something else.
func TestBranchEndpointsBecomeIndices(t *testing.T) {
	got := build(t, twoBus())

	if got.GetBranches()[0].GetFromBus() != 0 || got.GetBranches()[0].GetToBus() != 1 {
		t.Errorf("branch runs %d → %d",
			got.GetBranches()[0].GetFromBus(), got.GetBranches()[0].GetToBus())
	}
}

func TestBusKindsMapToTheWireEnumeration(t *testing.T) {
	got := build(t, twoBus())

	if got.GetBuses()[0].GetKind() != solverv1.BusKind_BUS_KIND_SLACK {
		t.Errorf("the slack is %v", got.GetBuses()[0].GetKind())
	}

	if got.GetBuses()[1].GetKind() != solverv1.BusKind_BUS_KIND_PQ {
		t.Errorf("the load bus is %v", got.GetBuses()[1].GetKind())
	}
}

// A zero base would divide every impedance by zero, and a missing bus would
// send an index the solver reads as another bus entirely.
func TestAMissingBaseOrBusIsRefused(t *testing.T) {
	t.Run("no power base", func(t *testing.T) {
		broken := twoBus()
		broken.SBaseVA = 0

		if _, err := wire.Model("x", broken); err == nil {
			t.Error("a model with no power base was converted")
		}
	})

	t.Run("no voltage base", func(t *testing.T) {
		broken := twoBus()
		broken.VBaseV = 0

		if _, err := wire.Model("x", broken); err == nil {
			t.Error("a model with no voltage base was converted")
		}
	})

	t.Run("dangling branch", func(t *testing.T) {
		broken := twoBus()
		broken.Branches[0].ToID = "bus-9"

		_, err := wire.Model("x", broken)
		if err == nil {
			t.Fatal("a branch to a bus that is not in the model was converted")
		}

		if !strings.Contains(err.Error(), "bus-9") {
			t.Errorf("the message does not name the missing bus: %v", err)
		}
	})

	t.Run("no buses", func(t *testing.T) {
		if _, err := wire.Model("x", canonical.Model{SBaseVA: sBase, VBaseV: vBase}); err == nil {
			t.Error("an empty model was converted")
		}
	})
}

// The one bus field that is SI on both sides, so it is copied rather than
// multiplied. Multiplying it by the base — which every other voltage on this
// bus goes through — would put a 138 kV bus at 19 gigavolts.
func TestANominalVoltageCrossesUnconverted(t *testing.T) {
	in := twoBus()
	in.Buses[0].VNominalV = 345e3
	in.Buses[1].VNominalV = 138e3

	got := build(t, in)

	want := []float64{345e3, 138e3}

	for position, volts := range want {
		bus := got.GetBuses()[position]

		if bus.VNominalV == nil {
			t.Fatalf("%s states no nominal voltage, and the model gave one", bus.GetId())
		}

		if bus.GetVNominalV() != volts {
			t.Errorf("%s is nominally %v V, want %v", bus.GetId(), bus.GetVNominalV(), volts)
		}
	}
}

// Absent stays absent, and this is the field's whole safety argument. Zero
// volts converts to a rating of zero, and every consumer of rating_va reads a
// non-positive rating as UNRATED and skips the branch — so a bus sent at zero
// would take its lines out of every violation check rather than fail one
// anywhere a reader could see.
func TestABusThatStatesNoNominalVoltageSendsNone(t *testing.T) {
	got := build(t, twoBus())

	for _, bus := range got.GetBuses() {
		if bus.VNominalV != nil {
			t.Errorf("%s came out claiming %v V from a model that states none",
				bus.GetId(), bus.GetVNominalV())
		}
	}
}

// An infinity is not a voltage, and it used to pass this boundary. The guard
// here was `> 0`, which +Inf satisfies — so a bus whose voltage had been
// computed by division crossed onto the wire, and the failure surfaced two
// packages later in the viewer, where encoding/json refuses to marshal one.
// Now the bus decides, with the same predicate the rest of the system uses.
func TestAnUnusableNominalVoltageDoesNotReachTheWire(t *testing.T) {
	for name, volts := range map[string]float64{
		"positive infinity": math.Inf(1),
		"negative infinity": math.Inf(-1),
		"negative":          -138e3,
		"not a number":      math.NaN(),
	} {
		t.Run(name, func(t *testing.T) {
			in := twoBus()
			in.Buses[0].VNominalV = volts

			got := build(t, in)

			if bus := got.GetBuses()[0]; bus.VNominalV != nil {
				t.Errorf("%v V crossed onto the wire as %v", volts, bus.GetVNominalV())
			}
		})
	}
}
