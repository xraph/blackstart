package matpower_test

import (
	"math"
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/networkmodel/canonical"
	"github.com/xraph/blackstart/extensions/networkmodel/matpower"
)

// A three-bus case in the shape real MATPOWER files take: comments, a scalar
// base, and three matrices spanning lines.
const threeBus = `
function mpc = case3
%% MATPOWER Case Format : Version 2
mpc.version = '2';

%%-----  Power Flow Data  -----%%
mpc.baseMVA = 100;

%% bus data
%	bus_i	type	Pd	Qd	Gs	Bs	area	Vm	Va	baseKV	zone	Vmax	Vmin
mpc.bus = [
	1	3	0	0	0	0	1	1.06	0	138	1	1.06	0.94;
	2	2	21.7	12.7	0	0	1	1.045	-4.98	138	1	1.06	0.94;
	3	1	94.2	19.0	0	19	1	1.010	-12.72	138	1	1.06	0.94;
];

%% generator data
%	bus	Pg	Qg	Qmax	Qmin	Vg	mBase	status	Pmax	Pmin
mpc.gen = [
	1	232.4	-16.9	10	-10	1.06	100	1	332.4	0;
	2	40.0	42.4	50	-40	1.045	100	1	140.0	0;
];

%% branch data
%	fbus	tbus	r	x	b	rateA	rateB	rateC	ratio	angle	status
mpc.branch = [
	1	2	0.01938	0.05917	0.0528	200	0	0	0	0	1;
	2	3	0.04699	0.19797	0.0438	150	0	0	0	0	1;
	1	3	0.05403	0.22304	0.0492	175	0	0	0.98	3.0	1;
];
`

func parse(t *testing.T, source string) matpower.Case {
	t.Helper()

	got, err := matpower.Parse(source)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	return got
}

func TestAThreeBusCaseParses(t *testing.T) {
	got := parse(t, threeBus)

	if got.BaseMVA != 100 {
		t.Errorf("baseMVA %v", got.BaseMVA)
	}

	if len(got.Model.Buses) != 3 {
		t.Fatalf("got %d buses, want three", len(got.Model.Buses))
	}

	if len(got.Model.Branches) != 3 {
		t.Fatalf("got %d branches, want three", len(got.Model.Branches))
	}
}

// Every value is converted against the file's own base, because that is what
// the canonical model carries. A missed conversion produces a plausible
// network on the wrong scale rather than an error.
func TestLoadsAndGenerationComeOutInPerUnit(t *testing.T) {
	got := parse(t, threeBus)

	byID := map[string]canonical.Bus{}
	for _, bus := range got.Model.Buses {
		byID[bus.ID] = bus
	}

	// Bus 3 draws 94.2 MW on a 100 MVA base: −0.942 per unit.
	if near := byID["bus-3"].PInjection; math.Abs(near+0.942) > 1e-9 {
		t.Errorf("bus 3 injection is %v, want -0.942", near)
	}

	// Bus 2 generates 40 and draws 21.7: +0.183 net.
	if near := byID["bus-2"].PInjection; math.Abs(near-0.183) > 1e-9 {
		t.Errorf("bus 2 injection is %v, want 0.183", near)
	}

	// Bus 3 has a 19 MVAr capacitor: +0.19 per unit susceptance.
	if near := byID["bus-3"].BShunt; math.Abs(near-0.19) > 1e-9 {
		t.Errorf("bus 3 shunt is %v, want 0.19", near)
	}
}

// The slack's injection is whatever the network requires. Seeding it from the
// file would make the imported case carry a solution it has not computed.
func TestTheSlacksInjectionIsNotSeededFromTheFile(t *testing.T) {
	got := parse(t, threeBus)

	for _, bus := range got.Model.Buses {
		if bus.ID == "bus-1" {
			if bus.PInjection != 0 {
				t.Errorf("the slack came in carrying %v, which is the file's answer",
					bus.PInjection)
			}

			if bus.Kind != 3 {
				t.Errorf("bus 1 has kind %d, want slack", bus.Kind)
			}
		}
	}
}

// Angles are degrees in MATPOWER and radians everywhere here. Getting that
// wrong is a factor of 57 that still solves.
func TestAnglesAreConvertedToRadians(t *testing.T) {
	got := parse(t, threeBus)

	for _, branch := range got.Model.Branches {
		if branch.Shift != 0 {
			// The third branch has a 3-degree shift.
			if math.Abs(branch.Shift-3*math.Pi/180) > 1e-12 {
				t.Errorf("shift is %v radians, want 3 degrees", branch.Shift)
			}
		}
	}

	byID := map[string]canonical.Bus{}
	for _, bus := range got.Model.Buses {
		byID[bus.ID] = bus
	}

	if near := byID["bus-2"].VAngle; math.Abs(near-(-4.98*math.Pi/180)) > 1e-12 {
		t.Errorf("bus 2 angle is %v radians, want -4.98 degrees", near)
	}
}

// MATPOWER writes 0 for "no transformer" and 1.0 is the identity. A zero ratio
// would divide the admittance by zero.
func TestAZeroRatioBecomesTheIdentity(t *testing.T) {
	got := parse(t, threeBus)

	for _, branch := range got.Model.Branches {
		if branch.Tap <= 0 {
			t.Errorf("branch %s has ratio %v", branch.ID, branch.Tap)
		}
	}

	// The third branch states 0.98 and must keep it.
	found := false

	for _, branch := range got.Model.Branches {
		if math.Abs(branch.Tap-0.98) < 1e-12 {
			found = true
		}
	}

	if !found {
		t.Error("the stated 0.98 ratio was lost")
	}
}

// Ratings convert against the case's own base, like every other quantity.
//
// This divided by a literal 100 until a test used a different base. Every
// MATPOWER case in the repository states 100 MVA, so the bug was invisible to
// all of them — and a rating wrong by a constant factor makes N-1 report
// overloads that are not there, or miss ones that are.
func TestRatingsUseTheCasesOwnBase(t *testing.T) {
	// The same network on a 250 MVA base. A 200 MVA rating is 0.8 per unit
	// here and would be 2.0 against a hardcoded 100.
	onBase250 := strings.Replace(threeBus, "mpc.baseMVA = 100;", "mpc.baseMVA = 250;", 1)

	got := parse(t, onBase250)

	if got.BaseMVA != 250 {
		t.Fatalf("baseMVA is %v", got.BaseMVA)
	}

	var found bool

	for _, branch := range got.Model.Branches {
		if branch.ID != "branch-0" {
			continue
		}

		found = true

		if math.Abs(branch.Rating-0.8) > 1e-12 {
			t.Errorf("a 200 MVA rating on a 250 MVA base is %v per unit, want 0.8",
				branch.Rating)
		}
	}

	if !found {
		t.Fatal("branch-0 is missing")
	}
}

// A case whose buses are not all at one voltage, which is the case the
// per-bus base exists for. Bus 1 is at 345 kV, buses 2 and 3 at 138 kV, and
// bus 4 states nothing at all.
const twoVoltage = `
mpc.baseMVA = 100;
mpc.bus = [
	1	3	0	0	0	0	1	1.0	0	345	1	1.06	0.94;
	2	1	50	10	0	0	1	1.0	0	138	1	1.06	0.94;
	3	1	40	10	0	0	1	1.0	0	138	1	1.06	0.94;
	4	1	10	2	0	0	1	1.0	0	0	1	1.06	0.94;
];
mpc.gen = [
	1	100	0	10	-10	1.0	100	1	200	0;
];
mpc.branch = [
	1	2	0.01	0.05	0.05	200	0	0	0	0	1;
	2	3	0.02	0.09	0.04	150	0	0	0	0	1;
];
`

// Each bus keeps the base its own row states. The importer read this column
// and discarded everything but the first value, which is how a 138 kV line
// came to be rated as though it were at 345 kV: 2.5 times too high, in
// correct arithmetic, on an input nobody could see was wrong.
func TestEachBusKeepsItsOwnVoltageBase(t *testing.T) {
	got := parse(t, twoVoltage)

	byID := map[string]canonical.Bus{}
	for _, bus := range got.Model.Buses {
		byID[bus.ID] = bus
	}

	want := map[string]float64{
		"bus-1": 345e3,
		"bus-2": 138e3,
		"bus-3": 138e3,
		// Stated as zero in the file, which is not a bus at zero volts but a
		// bus that says nothing. It must stay zero rather than inherit the
		// first bus's 345 kV.
		"bus-4": 0,
	}

	for id, volts := range want {
		if got := byID[id].VNominalV; got != volts {
			t.Errorf("%s is nominally %v V, want %v", id, got, volts)
		}
	}
}

// The model-level base still exists and still comes from the first bus that
// states one. The per-unit system needs exactly one, and this fix did not
// change which one it is — only whether the narrowing loses the rest.
func TestTheModelBaseIsStillTheFirstStatedOne(t *testing.T) {
	got := parse(t, twoVoltage)

	if got.Model.VBaseV != 345e3 {
		t.Errorf("model base is %v V, want the first bus's 345 kV", got.Model.VBaseV)
	}
}

// A case where no bus states a base at all: every bus reports unknown, and
// the model falls back to 1 volt so a per-unit conversion divides by
// something visibly wrong rather than by zero.
func TestNoStatedBaseLeavesEveryBusUnknown(t *testing.T) {
	silent := strings.NewReplacer("345", "0", "138", "0").Replace(twoVoltage)

	got := parse(t, silent)

	for _, bus := range got.Model.Buses {
		if bus.VNominalV != 0 {
			t.Errorf("%s claims %v V from a file that states none", bus.ID, bus.VNominalV)
		}
	}

	if got.Model.VBaseV != 1 {
		t.Errorf("model base is %v V, want 1", got.Model.VBaseV)
	}
}
