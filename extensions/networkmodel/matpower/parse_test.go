package matpower_test

import (
	"math"
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
