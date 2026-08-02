package reduce_test

import (
	"maps"
	"strconv"
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/topology/reduce"
)

// The four substation configurations that actually get built, reduced under
// the switch states that matter. These are the tests that decide whether this
// package is correct, because a reduction that works on a two-node toy and
// fails on a breaker-and-a-half is a reduction that works on nothing real.
//
// Every case states the expected bus count and why, so a change in the answer
// has to be argued with rather than re-baselined.

// closed marks every named device closed and everything else open.
//
// Explicit rather than relying on the unknown-is-closed default, because these
// tests are about which devices are open and leaving that implicit would make
// them read as though nothing were open at all.
func states(closed ...string) reduce.SwitchStates {
	out := reduce.SwitchStates{}
	for _, id := range closed {
		out[id] = true
	}

	return out
}

// withAllOpen returns states where every named device is open.
func open(all reduce.SwitchStates, opened ...string) reduce.SwitchStates {
	out := reduce.SwitchStates{}
	maps.Copy(out, all)

	for _, id := range opened {
		out[id] = false
	}

	return out
}

func node(id string) reduce.ConnectivityNode {
	return reduce.ConnectivityNode{ID: id, VoltageLevelID: "vl-230"}
}

func breaker(id, from, to string) reduce.Equipment {
	return reduce.Equipment{ID: id, Kind: reduce.KindBreaker, Terminals: []string{from, to}}
}

func line(id, from, to string) reduce.Equipment {
	return reduce.Equipment{ID: id, Kind: reduce.KindLine, Terminals: []string{from, to}}
}

// --- Ring bus ---
//
// Four line positions around a ring of four breakers. Every breaker closed is
// one bus; open one and it is still one bus, because a ring has two paths
// between any pair — which is the entire point of building one.

func ringBus() reduce.Snapshot {
	return reduce.Snapshot{
		Nodes: []reduce.ConnectivityNode{
			node("n1"), node("n2"), node("n3"), node("n4"),
			node("ext-a"), node("ext-b"), node("ext-c"), node("ext-d"),
		},
		Equipment: []reduce.Equipment{
			breaker("cb1", "n1", "n2"),
			breaker("cb2", "n2", "n3"),
			breaker("cb3", "n3", "n4"),
			breaker("cb4", "n4", "n1"),
			line("line-a", "n1", "ext-a"),
			line("line-b", "n2", "ext-b"),
			line("line-c", "n3", "ext-c"),
			line("line-d", "n4", "ext-d"),
		},
	}
}

func TestARingBusCollapsesToOneBus(t *testing.T) {
	got, err := reduce.Reduce(ringBus(), states("cb1", "cb2", "cb3", "cb4"))
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}

	// One ring bus plus four external nodes, each its own bus.
	if len(got.Buses) != 5 {
		t.Fatalf("got %d buses, want the ring plus four externals:\n%s",
			len(got.Buses), describe(got))
	}

	if got.BusOf("n1") != got.BusOf("n3") {
		t.Error("opposite corners of a closed ring landed on different buses")
	}
}

// The property a ring bus is built for: any one breaker can be taken out for
// maintenance without splitting the bus, because the other three still join
// every position.
func TestARingBusSurvivesOneOpenBreaker(t *testing.T) {
	all := states("cb1", "cb2", "cb3", "cb4")

	for _, opened := range []string{"cb1", "cb2", "cb3", "cb4"} {
		got, err := reduce.Reduce(ringBus(), open(all, opened))
		if err != nil {
			t.Fatalf("Reduce with %s open: %v", opened, err)
		}

		if len(got.Buses) != 5 {
			t.Errorf("opening %s split the ring into %d buses:\n%s",
				opened, len(got.Buses), describe(got))
		}
	}
}

// Two opposite breakers open does split it, into two halves of two positions.
// A ring survives one outage, not two, and a reduction that said otherwise
// would be hiding a real loss of redundancy.
func TestARingBusSplitsWhenTwoOppositeBreakersOpen(t *testing.T) {
	got, err := reduce.Reduce(ringBus(), open(states("cb1", "cb2", "cb3", "cb4"), "cb1", "cb3"))
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}

	if len(got.Buses) != 6 {
		t.Fatalf("got %d buses, want the ring split in two plus four externals:\n%s",
			len(got.Buses), describe(got))
	}

	if got.BusOf("n1") == got.BusOf("n3") {
		t.Error("the ring did not split with two opposite breakers open")
	}

	// The halves are n1-n4 through cb4 and n2-n3 through cb2. Naming which
	// breaker joins which pair, because getting that backwards is exactly the
	// mistake this case exists to catch — and the first draft of this test made
	// it.
	if got.BusOf("n1") != got.BusOf("n4") {
		t.Error("n1 and n4 should still be joined by cb4")
	}

	if got.BusOf("n2") != got.BusOf("n3") {
		t.Error("n2 and n3 should still be joined by cb2")
	}
}

// --- Breaker-and-a-half ---
//
// Two main buses with a diameter of three breakers between them. Two circuits
// tap the diameter at the two points between breakers, so each circuit has
// one and a half breakers to itself — hence the name.

func breakerAndAHalf() reduce.Snapshot {
	return reduce.Snapshot{
		Nodes: []reduce.ConnectivityNode{
			node("bus-1"), node("bus-2"),
			node("mid-a"), node("mid-b"),
			node("ext-a"), node("ext-b"),
		},
		Equipment: []reduce.Equipment{
			breaker("cb-1a", "bus-1", "mid-a"),
			breaker("cb-ab", "mid-a", "mid-b"),
			breaker("cb-b2", "mid-b", "bus-2"),
			line("line-a", "mid-a", "ext-a"),
			line("line-b", "mid-b", "ext-b"),
		},
	}
}

func TestABreakerAndAHalfDiameterCollapsesFully(t *testing.T) {
	got, err := reduce.Reduce(breakerAndAHalf(), states("cb-1a", "cb-ab", "cb-b2"))
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}

	// Both main buses, both mid-points and each other join into one bus; the
	// two external nodes stay separate.
	if len(got.Buses) != 3 {
		t.Fatalf("got %d buses, want one plus two externals:\n%s",
			len(got.Buses), describe(got))
	}

	if got.BusOf("bus-1") != got.BusOf("bus-2") {
		t.Error("a fully closed diameter did not join both main buses")
	}
}

// The configuration's whole purpose: the center breaker can be taken out and
// both circuits stay energised, one from each main bus.
func TestABreakerAndAHalfKeepsBothCircuitsWhenTheCentreBreakerOpens(t *testing.T) {
	got, err := reduce.Reduce(breakerAndAHalf(),
		open(states("cb-1a", "cb-ab", "cb-b2"), "cb-ab"))
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}

	// bus-1 with mid-a, bus-2 with mid-b, and the two externals.
	if len(got.Buses) != 4 {
		t.Fatalf("got %d buses, want two halves plus two externals:\n%s",
			len(got.Buses), describe(got))
	}

	if got.BusOf("bus-1") != got.BusOf("mid-a") {
		t.Error("line A lost its connection to bus 1")
	}

	if got.BusOf("bus-2") != got.BusOf("mid-b") {
		t.Error("line B lost its connection to bus 2")
	}

	if got.BusOf("bus-1") == got.BusOf("bus-2") {
		t.Error("the main buses are still joined with the center breaker open")
	}
}

// --- Double bus, double breaker ---
//
// Every circuit has its own breaker to each of two main buses. The most
// expensive arrangement and the most forgiving: either main bus can be taken
// out entirely and every circuit stays connected through the other.

func doubleBusDoubleBreaker() reduce.Snapshot {
	return reduce.Snapshot{
		Nodes: []reduce.ConnectivityNode{
			node("bus-1"), node("bus-2"),
			node("tap-a"), node("tap-b"),
			node("ext-a"), node("ext-b"),
		},
		Equipment: []reduce.Equipment{
			breaker("cb-a1", "tap-a", "bus-1"),
			breaker("cb-a2", "tap-a", "bus-2"),
			breaker("cb-b1", "tap-b", "bus-1"),
			breaker("cb-b2", "tap-b", "bus-2"),
			line("line-a", "tap-a", "ext-a"),
			line("line-b", "tap-b", "ext-b"),
		},
	}
}

func TestDoubleBusDoubleBreakerCollapsesFully(t *testing.T) {
	got, err := reduce.Reduce(doubleBusDoubleBreaker(),
		states("cb-a1", "cb-a2", "cb-b1", "cb-b2"))
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}

	if len(got.Buses) != 3 {
		t.Fatalf("got %d buses, want one plus two externals:\n%s",
			len(got.Buses), describe(got))
	}
}

// The property that justifies the cost: a whole main bus out of service and
// every circuit still connected, through its second breaker.
func TestDoubleBusDoubleBreakerKeepsEveryCircuitWhenOneBusIsOut(t *testing.T) {
	got, err := reduce.Reduce(doubleBusDoubleBreaker(),
		open(states("cb-a1", "cb-a2", "cb-b1", "cb-b2"), "cb-a1", "cb-b1"))
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}

	// bus-1 is isolated and becomes its own bus; everything else joins through
	// bus-2. Plus the two externals.
	if got.BusOf("tap-a") != got.BusOf("tap-b") {
		t.Error("the two circuits lost their common connection through bus 2")
	}

	if got.BusOf("bus-1") == got.BusOf("tap-a") {
		t.Error("bus 1 is still connected with both its breakers open")
	}

	// An isolated main bus is reported, not dropped: it is a real part of the
	// network in a real state.
	if got.BusOf("bus-1") == 0 {
		t.Error("the isolated main bus vanished from the reduction")
	}
}

// --- Bus section with a tie breaker ---
//
// The case the plan calls out specifically: closed gives one bus, open gives
// two. The simplest configuration here and the one whose answer is most
// obviously checkable, which is why it is worth having alongside the others.

func sectionedBus() reduce.Snapshot {
	return reduce.Snapshot{
		Nodes: []reduce.ConnectivityNode{
			node("sec-1"), node("sec-2"), node("ext-a"), node("ext-b"),
		},
		Equipment: []reduce.Equipment{
			breaker("tie", "sec-1", "sec-2"),
			line("line-a", "sec-1", "ext-a"),
			line("line-b", "sec-2", "ext-b"),
		},
	}
}

func TestATieBreakerDecidesWhetherASectionedBusIsOneBusOrTwo(t *testing.T) {
	joined, err := reduce.Reduce(sectionedBus(), states("tie"))
	if err != nil {
		t.Fatalf("Reduce with the tie closed: %v", err)
	}

	if joined.BusOf("sec-1") != joined.BusOf("sec-2") {
		t.Error("a closed tie breaker did not join the two sections")
	}

	split, err := reduce.Reduce(sectionedBus(), open(states("tie"), "tie"))
	if err != nil {
		t.Fatalf("Reduce with the tie open: %v", err)
	}

	if split.BusOf("sec-1") == split.BusOf("sec-2") {
		t.Error("an open tie breaker left the two sections joined")
	}

	if len(split.Buses) != len(joined.Buses)+1 {
		t.Errorf("opening the tie went from %d buses to %d, want one more",
			len(joined.Buses), len(split.Buses))
	}
}

// describe renders a reduction for a failure message.
//
// A bus count alone says a test failed; this says how, which is the difference
// between a message that starts an investigation and one that ends it.
func describe(r reduce.Reduction) string {
	out := ""

	for _, bus := range r.Buses {
		out += "  bus " + strconv.Itoa(bus.Number) + ": " +
			strings.Join(bus.Nodes, ", ") + "\n"
	}

	return out
}
