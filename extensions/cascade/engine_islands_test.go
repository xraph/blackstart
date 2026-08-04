package cascade_test

// Tests for the per-island and per-event half of fromWire in engine.go, kept
// in their own file because this is where the frequency fields live — the
// one place in the whole crossing where a division would be wrong but still
// look plausible.

import (
	"testing"

	"github.com/xraph/blackstart/extensions/cascade"
	solverv1 "github.com/xraph/blackstart/proto/gen/go/blackstart/solver/v1"
)

func islandWire(frequency, nadir, rocof *float64) *solverv1.IslandState {
	return &solverv1.IslandState{
		Buses:       []uint32{0, 1},
		Condition:   solverv1.IslandCondition_ISLAND_CONDITION_SHEDDING,
		Verdict:     solverv1.AcVerdict_AC_VERDICT_VIOLATED,
		BalanceW:    -20e6,
		FrequencyHz: frequency,
		NadirHz:     nadir,
		RocofHzS:    rocof,
		ShedW:       10e6,
	}
}

func withResponseIsland(island *solverv1.IslandState) *solverv1.SimulateCascadeResponse {
	response := settledResponse()
	response.Islands = []*solverv1.IslandState{island}

	return response
}

// The sharpest edge in this crossing: a frequency is HERTZ on both sides of
// the wire and must never be divided by the power base. 59.4 Hz in must come
// back as 59.4, not 0.594.
func TestAFrequencyIsNotDividedByTheBase(t *testing.T) {
	frequency := 59.4
	nadir := 58.9
	rocof := -1.2

	simulator := &fakeSimulator{response: withResponseIsland(islandWire(&frequency, &nadir, &rocof))}

	result, err := engine(t, simulator, nil).Simulate(t.Context(), cascade.Request{Model: model()})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}

	island := result.Islands[0]

	if island.Frequency == nil || *island.Frequency != 59.4 {
		t.Errorf("frequency is %v, want 59.4 Hz; a power base leaked in", island.Frequency)
	}

	if island.Nadir == nil || *island.Nadir != 58.9 {
		t.Errorf("nadir is %v, want 58.9 Hz", island.Nadir)
	}

	if island.Rocof == nil || *island.Rocof != -1.2 {
		t.Errorf("rocof is %v, want -1.2 Hz/s", island.Rocof)
	}
}

// The absence this whole design protects. An island with no machine data has
// no frequency to report, and a zero here would be read as a real, if
// unusually round, measurement.
func TestAnAbsentFrequencyStaysNil(t *testing.T) {
	simulator := &fakeSimulator{response: withResponseIsland(islandWire(nil, nil, nil))}

	result, err := engine(t, simulator, nil).Simulate(t.Context(), cascade.Request{Model: model()})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}

	island := result.Islands[0]

	if island.Frequency != nil {
		t.Errorf("frequency is %v, want nil", *island.Frequency)
	}

	if island.Nadir != nil {
		t.Errorf("nadir is %v, want nil", *island.Nadir)
	}

	if island.Rocof != nil {
		t.Errorf("rocof is %v, want nil", *island.Rocof)
	}
}

// Balance and shed are watts on the wire and per unit here, the same
// crossing load_lost_w makes at the run level.
func TestIslandPowerComesBackInPerUnit(t *testing.T) {
	simulator := &fakeSimulator{response: withResponseIsland(islandWire(nil, nil, nil))}

	result, err := engine(t, simulator, nil).Simulate(t.Context(), cascade.Request{Model: model()})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}

	island := result.Islands[0]

	if island.Balance != -0.2 {
		t.Errorf("balance is %v, want -0.2 pu; SI escaped", island.Balance)
	}

	if island.Shed != 0.1 {
		t.Errorf("shed is %v, want 0.1 pu; SI escaped", island.Shed)
	}

	if island.BusCount != 2 {
		t.Errorf("bus count is %d, want 2", island.BusCount)
	}
}

// Condition and verdict are separate axes on purpose — see IslandState in
// the contract — and both have to survive the crossing independently.
func TestIslandConditionAndVerdictCrossTheWire(t *testing.T) {
	simulator := &fakeSimulator{response: withResponseIsland(islandWire(nil, nil, nil))}

	result, err := engine(t, simulator, nil).Simulate(t.Context(), cascade.Request{Model: model()})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}

	island := result.Islands[0]

	if island.Condition != "shedding" {
		t.Errorf("condition is %q, want shedding", island.Condition)
	}

	if island.Verdict != "violated" {
		t.Errorf("verdict is %q, want violated", island.Verdict)
	}
}

// Proto3 cannot distinguish an unset enum from a deliberate zero. Reading an
// uncategorized island as ConditionStable would be the most reassuring wrong
// answer this package could give.
func TestAnUnspecifiedConditionIsNotReadAsStable(t *testing.T) {
	island := islandWire(nil, nil, nil)
	island.Condition = solverv1.IslandCondition_ISLAND_CONDITION_UNSPECIFIED
	island.Verdict = solverv1.AcVerdict_AC_VERDICT_UNSPECIFIED

	simulator := &fakeSimulator{response: withResponseIsland(island)}

	result, err := engine(t, simulator, nil).Simulate(t.Context(), cascade.Request{Model: model()})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}

	got := result.Islands[0]

	if got.Condition != "" {
		t.Errorf("an unset condition came back as %q", got.Condition)
	}

	if got.Verdict != "" {
		t.Errorf("an unset verdict came back as %q", got.Verdict)
	}
}

// Events carry a simulated-time offset in seconds, never divided by the
// power base, and an island reference that is absent — not zero — before
// the network has split.
func TestAnEventsAbsentIslandStaysNil(t *testing.T) {
	response := settledResponse()
	response.Events = []*solverv1.CascadeEvent{{
		AtS:     12.5,
		Kind:    solverv1.CascadeEventKind_CASCADE_EVENT_KIND_OVERLOAD,
		Element: "ln TULIA-PLAINVIEW 345",
		Detail:  "163% - 2s",
		// Island deliberately left unset: this overload predates any split.
	}}

	simulator := &fakeSimulator{response: response}

	result, err := engine(t, simulator, nil).Simulate(t.Context(), cascade.Request{Model: model()})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}

	event := result.Events[0]

	if event.At != 12.5 {
		t.Errorf("event time is %v, want 12.5s; a base leaked into simulated time", event.At)
	}

	if event.Kind != "overload" {
		t.Errorf("event kind is %q, want overload", event.Kind)
	}

	if event.Island != nil {
		t.Errorf("island is %v, want nil for a pre-split event", *event.Island)
	}
}

// The island a later event belongs to must survive the crossing as the same
// number the solver reported, including island zero — which is not the same
// as absent.
func TestAnEventsIslandZeroIsNotConfusedWithAbsent(t *testing.T) {
	response := settledResponse()
	zero := uint32(0)
	response.Events = []*solverv1.CascadeEvent{{
		Kind:   solverv1.CascadeEventKind_CASCADE_EVENT_KIND_SHED,
		Island: &zero,
	}}

	simulator := &fakeSimulator{response: response}

	result, err := engine(t, simulator, nil).Simulate(t.Context(), cascade.Request{Model: model()})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}

	event := result.Events[0]

	if event.Island == nil {
		t.Fatal("island 0 came back as absent")
	}

	if *event.Island != 0 {
		t.Errorf("island is %d, want 0", *event.Island)
	}

	if event.Kind != "shed" {
		t.Errorf("event kind is %q, want shed", event.Kind)
	}
}
