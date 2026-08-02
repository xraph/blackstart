package matpower_test

import (
	"math"
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/networkmodel/canonical"
)

// Several machines can sit on one bus and the canonical model has one
// injection per bus, so limits sum. A case where they did not would report a
// generator hitting a limit it does not have.
func TestGeneratorLimitsSumOntoTheirBus(t *testing.T) {
	source := strings.Replace(threeBus, `	2	40.0	42.4	50	-40	1.045	100	1	140.0	0;`,
		"\t2\t20.0\t21.2\t25\t-20\t1.045\t100\t1\t70.0\t0;\n\t2\t20.0\t21.2\t25\t-20\t1.045\t100\t1\t70.0\t0;", 1)

	got := parse(t, source)

	for _, bus := range got.Model.Buses {
		if bus.ID != "bus-2" {
			continue
		}

		// Two machines at ±25 MVAr sum to ±50, which is 0.5 per unit.
		if math.Abs(bus.QMax-0.5) > 1e-9 {
			t.Errorf("bus 2 upper limit is %v, want 0.5", bus.QMax)
		}

		// And their real generation sums back to the original 40 MW.
		if math.Abs(bus.PInjection-0.183) > 1e-9 {
			t.Errorf("bus 2 injection is %v after splitting the machine", bus.PInjection)
		}
	}
}

// A generator with no stated limits leaves its bus unlimited, not at zero. A
// zero would turn missing data into a voltage collapse indistinguishable from
// a real finding.
func TestABusWithNoGeneratorIsUnlimited(t *testing.T) {
	got := parse(t, threeBus)

	for _, bus := range got.Model.Buses {
		if bus.ID == "bus-3" {
			if !math.IsInf(bus.QMin, -1) || !math.IsInf(bus.QMax, 1) {
				t.Errorf("a load bus came in with limits %v..%v", bus.QMin, bus.QMax)
			}
		}
	}
}

// Identifiers derive from the MATPOWER bus number rather than being minted, so
// re-parsing an unchanged file produces the same hash. A minted identifier
// would give a different hash on every import and miss the cache every time.
func TestReparsingAnUnchangedFileGivesTheSameHash(t *testing.T) {
	first, err := canonical.Hash(parse(t, threeBus).Model)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	for range 10 {
		again, err := canonical.Hash(parse(t, threeBus).Model)
		if err != nil {
			t.Fatalf("Hash: %v", err)
		}

		if again != first {
			t.Fatalf("one file parsed to %q then %q", first, again)
		}
	}
}

// The source's own identifier is never discarded. Without it a re-import
// cannot recognize what it already has and an operator searching by the number
// on their diagram finds nothing.
func TestTheSourcesOwnNumbersAreKept(t *testing.T) {
	got := parse(t, threeBus)

	if got.Aliases["bus-2"] != "2" {
		t.Errorf("bus 2's alias is %q", got.Aliases["bus-2"])
	}

	if len(got.Aliases) < len(got.Model.Buses)+len(got.Model.Branches) {
		t.Errorf("got %d aliases for %d buses and %d branches",
			len(got.Aliases), len(got.Model.Buses), len(got.Model.Branches))
	}
}
