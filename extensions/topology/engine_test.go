package topology_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/topology"
	tid "github.com/xraph/blackstart/extensions/topology/id"
	"github.com/xraph/blackstart/extensions/topology/network"
	"github.com/xraph/blackstart/extensions/topology/reduce"
	"github.com/xraph/blackstart/extensions/topology/store/memory"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

func engine(t *testing.T) *topology.Engine {
	t.Helper()

	e, err := topology.NewEngine(memory.New(), nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	return e
}

// A bus section with a tie breaker, which is the configuration whose answer
// changes with one switch: closed is one bus, open is two. Built through the
// store rather than handed to reduce directly, because what is under test here
// is the assembly — the reduction itself is already tested on hand-built
// snapshots.
type tieBus struct {
	substation id.ID
	tie        id.ID
	west, east id.ID
}

func defineTieBus(t *testing.T, e *topology.Engine, tieClosed bool) tieBus {
	t.Helper()

	site := tid.NewSubstation()
	level := tid.NewVoltageLevel()
	west, east := tid.NewConnectivityNode(), tid.NewConnectivityNode()
	tie := tid.NewEquipment()
	lineWest, lineEast := tid.NewEquipment(), tid.NewEquipment()
	outerWest, outerEast := tid.NewConnectivityNode(), tid.NewConnectivityNode()

	node := func(nodeID id.ID) network.ConnectivityNode {
		return network.ConnectivityNode{ID: nodeID, VoltageLevelID: level}
	}

	terminal := func(equipment, on id.ID, seq int) network.Terminal {
		return network.Terminal{
			ID:                 tid.NewTerminal(),
			EquipmentID:        equipment,
			ConnectivityNodeID: on,
			Sequence:           seq,
		}
	}

	_, err := e.Define(t.Context(), topology.Definition{
		Substation: network.Substation{ID: site, Name: "Tie Bus"},
		Levels: []network.VoltageLevel{
			{ID: level, SubstationID: site, NominalKV: 230, Name: "230 kV"},
		},
		Nodes: []network.ConnectivityNode{
			node(west), node(east), node(outerWest), node(outerEast),
		},
		Equipment: []network.Equipment{
			{
				ID: tie, Kind: reduce.KindBreaker, VoltageLevelID: level,
				Name: "Tie", NormallyClosed: tieClosed,
			},
			{ID: lineWest, Kind: reduce.KindLine, VoltageLevelID: level, Name: "West line"},
			{ID: lineEast, Kind: reduce.KindLine, VoltageLevelID: level, Name: "East line"},
		},
		Terminals: []network.Terminal{
			terminal(tie, west, 0), terminal(tie, east, 1),
			terminal(lineWest, west, 0), terminal(lineWest, outerWest, 1),
			terminal(lineEast, east, 0), terminal(lineEast, outerEast, 1),
		},
		Aliases: []network.ExternalAlias{
			{
				ID: tid.NewExternalAlias(), EntityID: site,
				Namespace: "iec61850", Value: "TIEBUS",
			},
		},
	})
	if err != nil {
		t.Fatalf("Define: %v", err)
	}

	return tieBus{substation: site, tie: tie, west: west, east: east}
}

// The whole point of storing a node-breaker model: the answer depends on a
// switch, and the switch is read from the store rather than assumed.
func TestAClosedTieBreakerMergesTheTwoSections(t *testing.T) {
	e := engine(t)
	built := defineTieBus(t, e, true)

	reduced, err := e.Reduce(t.Context(), built.substation, nil)
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}

	if reduced.BusOf(built.west.String()) != reduced.BusOf(built.east.String()) {
		t.Errorf("a closed tie breaker left the sections on different buses")
	}
}

func TestAnOpenTieBreakerLeavesTwoSections(t *testing.T) {
	e := engine(t)
	built := defineTieBus(t, e, false)

	reduced, err := e.Reduce(t.Context(), built.substation, nil)
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}

	if reduced.BusOf(built.west.String()) == reduced.BusOf(built.east.String()) {
		t.Errorf("an open tie breaker still merged the sections")
	}
}

// An override answers "what if" without restating the model, which is what a
// contingency study does thousands of times.
func TestAnOverrideOpensABreakerThatIsNormallyClosed(t *testing.T) {
	e := engine(t)
	built := defineTieBus(t, e, true)

	reduced, err := e.Reduce(t.Context(), built.substation,
		topology.Overrides{built.tie: false})
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}

	if reduced.BusOf(built.west.String()) == reduced.BusOf(built.east.String()) {
		t.Error("the override did not open the breaker")
	}
}

// An override naming a device that is not there is a caller asking about a
// different substation, and answering anyway would answer a different question
// with no sign it had.
func TestAnOverrideNamingAbsentEquipmentIsRefused(t *testing.T) {
	e := engine(t)
	built := defineTieBus(t, e, true)

	_, err := e.Reduce(t.Context(), built.substation,
		topology.Overrides{tid.NewEquipment(): false})
	if err == nil {
		t.Fatal("Reduce accepted an override for equipment that is not there")
	}

	if !strings.Contains(err.Error(), "not a switchable device") {
		t.Errorf("the message does not say why: %v", err)
	}
}

// A line is not switchable, so its state is not a knob. Overriding it would
// look like it worked and change nothing.
func TestALineCannotBeOverridden(t *testing.T) {
	e := engine(t)
	built := defineTieBus(t, e, true)

	reduced, err := e.Reduce(t.Context(), built.substation, nil)
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}

	if len(reduced.Branches) != 2 {
		t.Fatalf("got %d branches, want the two lines", len(reduced.Branches))
	}

	for _, branch := range reduced.Branches {
		lineID, err := tid.ParseEquipment(branch.EquipmentID)
		if err != nil {
			t.Fatalf("branch %s is not an equipment identifier", branch.EquipmentID)
		}

		if _, err := e.Reduce(t.Context(), built.substation,
			topology.Overrides{lineID: false}); err == nil {
			t.Errorf("a line accepted a switch state")
		}
	}
}

// A substation that is not there and a substation that is empty are different
// things, and reporting the first as the second hides a typo.
func TestAMissingSubstationIsNotFound(t *testing.T) {
	e := engine(t)

	if _, err := e.Reduce(t.Context(), tid.NewSubstation(), nil); !errors.Is(err, bserr.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// An import must be able to find what it already wrote, by the name its source
// used rather than by an identifier the source never saw.
func TestASourceIdentifierResolvesBackToItsEntity(t *testing.T) {
	e := engine(t)
	built := defineTieBus(t, e, true)

	found, err := e.Resolve(t.Context(), "iec61850", "TIEBUS")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if found.EntityID != built.substation {
		t.Errorf("the alias resolved to %s, want %s", found.EntityID, built.substation)
	}

	if _, err := e.Resolve(t.Context(), "matpower", "TIEBUS"); !errors.Is(err, bserr.ErrNotFound) {
		t.Errorf("the same value in another namespace resolved: %v", err)
	}
}

// A terminal whose node was never written is a dangling reference in the
// structure the reduction walks, and it must be refused where it is made
// rather than reported far from its cause.
func TestATerminalToAnAbsentNodeIsRefused(t *testing.T) {
	e := engine(t)

	site, level := tid.NewSubstation(), tid.NewVoltageLevel()
	equipment := tid.NewEquipment()

	_, err := e.Define(t.Context(), topology.Definition{
		Substation: network.Substation{ID: site, Name: "Dangling"},
		Levels: []network.VoltageLevel{
			{ID: level, SubstationID: site, NominalKV: 230, Name: "230 kV"},
		},
		Equipment: []network.Equipment{
			{ID: equipment, Kind: reduce.KindLine, VoltageLevelID: level, Name: "Line"},
		},
		Terminals: []network.Terminal{
			{
				ID:                 tid.NewTerminal(),
				EquipmentID:        equipment,
				ConnectivityNodeID: tid.NewConnectivityNode(),
			},
		},
	})
	if err == nil {
		t.Fatal("Define accepted a terminal naming a node that does not exist")
	}

	if !errors.Is(err, bserr.ErrInvalid) {
		t.Errorf("got %v, want ErrInvalid", err)
	}
}
