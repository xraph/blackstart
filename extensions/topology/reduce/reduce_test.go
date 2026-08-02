package reduce_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/topology/reduce"
)

// Bus numbers feed the solver's content hash. A reduction that numbered the
// same network two ways would produce two hashes for one network, defeat the
// model cache, and make every golden file downstream of it noise. That makes
// determinism a correctness property here, not a nicety.
func TestNumberingIsIdenticalAcrossRuns(t *testing.T) {
	snapshot := sectionedBus()
	closed := states("tie")

	first, err := reduce.Reduce(snapshot, closed)
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}

	for range 50 {
		again, err := reduce.Reduce(snapshot, closed)
		if err != nil {
			t.Fatalf("Reduce: %v", err)
		}

		if len(again.Buses) != len(first.Buses) {
			t.Fatalf("bus count changed between runs: %d then %d",
				len(first.Buses), len(again.Buses))
		}

		for i := range first.Buses {
			if again.Buses[i].Number != first.Buses[i].Number ||
				strings.Join(again.Buses[i].Nodes, ",") != strings.Join(first.Buses[i].Nodes, ",") {
				t.Fatalf("bus %d changed between runs:\n%s\n%s",
					i, describe(first), describe(again))
			}
		}
	}
}

// The order equipment appears in the snapshot must not change the answer. Once
// snapshots come from a database rather than a literal, that order is whatever
// the query returned — and a reduction that depended on it would produce a
// different content hash for the same network on every read.
func TestTheSnapshotsOrderDoesNotChangeTheNumbering(t *testing.T) {
	forward := ringBus()

	reversed := ringBus()
	slices.Reverse(reversed.Equipment)
	slices.Reverse(reversed.Nodes)

	closed := states("cb1", "cb2", "cb3", "cb4")

	a, err := reduce.Reduce(forward, closed)
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}

	b, err := reduce.Reduce(reversed, closed)
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}

	if describe(a) != describe(b) {
		t.Errorf("reversing the snapshot changed the numbering:\n%s\n%s",
			describe(a), describe(b))
	}
}

// Bus numbers start at 1 and run without gaps. A gap would mean a bus was
// dropped somewhere between the union-find and the numbering, and the solver
// indexes by position.
func TestBusNumbersAreContiguousFromOne(t *testing.T) {
	got, err := reduce.Reduce(ringBus(), states("cb1", "cb2", "cb3", "cb4"))
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}

	for index, bus := range got.Buses {
		if bus.Number != index+1 {
			t.Fatalf("bus at position %d is numbered %d", index, bus.Number)
		}
	}
}

// Every node lands on exactly one bus. A node on none would vanish from the
// model; a node on two would mean the union-find left a class split.
func TestEveryNodeLandsOnExactlyOneBus(t *testing.T) {
	snapshot := breakerAndAHalf()

	got, err := reduce.Reduce(snapshot, states("cb-1a", "cb-ab", "cb-b2"))
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}

	seen := map[string]int{}

	for _, bus := range got.Buses {
		for _, node := range bus.Nodes {
			seen[node]++
		}
	}

	for _, node := range snapshot.Nodes {
		switch seen[node.ID] {
		case 1:
			// Correct.
		case 0:
			t.Errorf("node %s vanished from the reduction", node.ID)
		default:
			t.Errorf("node %s landed on %d buses", node.ID, seen[node.ID])
		}
	}
}

// An unknown device is treated as closed, and the direction of that default
// matters. Assuming open would silently split a substation and report islands
// that do not exist, which reads as a finding; assuming closed produces a bus
// with more equipment on it than expected, which is visible rather than
// alarming.
func TestAnUnknownDeviceIsAssumedClosed(t *testing.T) {
	// No states at all.
	got, err := reduce.Reduce(sectionedBus(), reduce.SwitchStates{})
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}

	if got.BusOf("sec-1") != got.BusOf("sec-2") {
		t.Error("an unstated tie breaker was treated as open")
	}
}

// A de-energized section is reported, never deleted. It is a real part of the
// network in a real state, and dropping it would mean the reduction quietly
// disagrees with the model it came from about how many buses exist.
func TestAnIsolatedSectionIsReportedRatherThanDropped(t *testing.T) {
	snapshot := reduce.Snapshot{
		Nodes: []reduce.ConnectivityNode{
			node("live-1"), node("live-2"), node("dead"), node("ext"),
		},
		Equipment: []reduce.Equipment{
			breaker("tie", "live-1", "live-2"),
			breaker("isolator", "live-2", "dead"),
			line("feeder", "live-1", "ext"),
		},
	}

	got, err := reduce.Reduce(snapshot, open(states("tie", "isolator"), "isolator"))
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}

	if got.BusOf("dead") == 0 {
		t.Fatal("the isolated section vanished from the reduction")
	}

	if got.BusOf("dead") == got.BusOf("live-1") {
		t.Error("the isolated section is still joined to the live one")
	}

	// And it is reported as its own island, since nothing with impedance
	// reaches it.
	if len(got.Islands) < 2 {
		t.Errorf("got %d islands, want the live network and the dead section",
			len(got.Islands))
	}
}

// A closed switch across a transformer's voltage levels is a modelling error:
// levels are separated by impedance, and merging across one would produce a
// bus at two voltages that no solver can represent.
func TestAClosedSwitchCannotJoinTwoVoltageLevels(t *testing.T) {
	snapshot := reduce.Snapshot{
		Nodes: []reduce.ConnectivityNode{
			{ID: "hv", VoltageLevelID: "vl-230"},
			{ID: "lv", VoltageLevelID: "vl-115"},
		},
		Equipment: []reduce.Equipment{breaker("wrong", "hv", "lv")},
	}

	_, err := reduce.Reduce(snapshot, states("wrong"))
	if err == nil {
		t.Fatal("Reduce merged two voltage levels across a closed switch")
	}

	if !strings.Contains(err.Error(), "transformer") {
		t.Errorf("the message does not say what should connect them: %v", err)
	}
}

// Structural problems are named before any merging, because the same problem
// found halfway through produces a partial reduction and a message pointing at
// the wrong place.
func TestAMalformedSnapshotIsRejectedWithItsCause(t *testing.T) {
	cases := map[string]struct {
		snapshot reduce.Snapshot
		mentions string
	}{
		"dangling terminal": {
			snapshot: reduce.Snapshot{
				Nodes:     []reduce.ConnectivityNode{node("a")},
				Equipment: []reduce.Equipment{breaker("cb", "a", "nowhere")},
			},
			mentions: "nowhere",
		},
		"duplicate node": {
			snapshot: reduce.Snapshot{
				Nodes: []reduce.ConnectivityNode{node("a"), node("a")},
			},
			mentions: "duplicate",
		},
		"switch with one terminal": {
			snapshot: reduce.Snapshot{
				Nodes: []reduce.ConnectivityNode{node("a")},
				Equipment: []reduce.Equipment{
					{ID: "cb", Kind: reduce.KindBreaker, Terminals: []string{"a"}},
				},
			},
			mentions: "two nodes",
		},
		"generator with two terminals": {
			snapshot: reduce.Snapshot{
				Nodes: []reduce.ConnectivityNode{node("a"), node("b")},
				Equipment: []reduce.Equipment{
					{ID: "gen", Kind: reduce.KindGenerator, Terminals: []string{"a", "b"}},
				},
			},
			mentions: "one node",
		},
		"unknown kind": {
			snapshot: reduce.Snapshot{
				Nodes: []reduce.ConnectivityNode{node("a")},
				Equipment: []reduce.Equipment{
					{ID: "thing", Kind: "reactor", Terminals: []string{"a"}},
				},
			},
			mentions: "reactor",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := reduce.Reduce(tc.snapshot, reduce.SwitchStates{})
			if err == nil {
				t.Fatalf("Reduce accepted a snapshot with a %s", name)
			}

			if !strings.Contains(err.Error(), tc.mentions) {
				t.Errorf("message %q does not mention %q", err, tc.mentions)
			}
		})
	}
}

// Generators and loads attach to whichever bus their node landed on, and come
// back sorted so a reduced model reads the same way every run.
func TestInjectionsAttachToTheirMergedBus(t *testing.T) {
	snapshot := reduce.Snapshot{
		Nodes: []reduce.ConnectivityNode{node("a"), node("b")},
		Equipment: []reduce.Equipment{
			breaker("tie", "a", "b"),
			{ID: "load-1", Kind: reduce.KindLoad, Terminals: []string{"a"}},
			{ID: "gen-1", Kind: reduce.KindGenerator, Terminals: []string{"b"}},
		},
	}

	got, err := reduce.Reduce(snapshot, states("tie"))
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}

	if len(got.Injections) != 2 {
		t.Fatalf("got %d injections, want two", len(got.Injections))
	}

	// Sorted by identifier: gen-1 before load-1.
	if got.Injections[0].EquipmentID != "gen-1" {
		t.Errorf("injections are not sorted: %v", got.Injections)
	}

	if got.Injections[0].Bus != got.Injections[1].Bus {
		t.Error("a closed tie left the generator and load on different buses")
	}
}
