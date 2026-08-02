package mapping_test

import (
	"os"
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/scl/mapping"
	"github.com/xraph/blackstart/extensions/scl/parse"
	"github.com/xraph/blackstart/extensions/topology"
	"github.com/xraph/blackstart/extensions/topology/reduce"
	"github.com/xraph/blackstart/extensions/topology/store/memory"
	"github.com/xraph/blackstart/pkg/id"
)

func ringBus(t *testing.T) []mapping.Result {
	t.Helper()

	file, err := os.Open("../parse/testdata/ring.scd")
	if err != nil {
		t.Fatalf("opening the fixture: %v", err)
	}
	defer func() { _ = file.Close() }()

	parsed, err := parse.Decode(file)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	mapped, err := mapping.Substations(parsed)
	if err != nil {
		t.Fatalf("Substations: %v", err)
	}

	return mapped
}

// The payoff for ConnectivityNode existing since Phase 1: SCL's nodes map
// one-to-one onto blackstart's, so the node-breaker detail survives import
// rather than being reduced away before anything can use it.
func TestConnectivityNodesMapOneToOne(t *testing.T) {
	got := ringBus(t)

	if len(got) != 1 {
		t.Fatalf("got %d substations", len(got))
	}

	if n := len(got[0].Definition.Nodes); n != 4 {
		t.Errorf("got %d connectivity nodes, want the file's four", n)
	}
}

// A current transformer measures the network without being part of it. Mapping
// one as a branch would put an impedance in the network that is not there —
// and the reason has to be reported, not silently dropped, so an operator
// comparing counts against their own tooling can see what was left out.
func TestMeasurementTransformersAreSkippedWithAReason(t *testing.T) {
	got := ringBus(t)[0]

	var measurement *mapping.Skip

	for index, skip := range got.Skipped {
		if skip.Type == "CTR" {
			measurement = &got.Skipped[index]
		}
	}

	if measurement == nil {
		t.Fatalf("the current transformer was not skipped; skipped %+v", got.Skipped)
	}

	if !strings.Contains(measurement.Reason, "impedance") {
		t.Errorf("the reason does not say why: %q", measurement.Reason)
	}

	for _, item := range got.Definition.Equipment {
		if item.Name == "CT1" {
			t.Error("the current transformer was mapped as electrical topology")
		}
	}
}

// A transformer sits at the voltage level rather than in a bay, because it
// joins two levels. A mapper that only walked bays would lose every one.
func TestALevelTransformerIsMapped(t *testing.T) {
	got := ringBus(t)[0]

	var found bool

	for _, item := range got.Definition.Equipment {
		if item.Name == "TR1" && item.Kind == reduce.KindTransformer {
			found = true
		}
	}

	if !found {
		t.Errorf("the level's transformer is missing from %d devices",
			len(got.Definition.Equipment))
	}
}

// Reading the value without its multiplier turns a 230 kV substation into a
// 230 volt one, which the reduction accepts because nothing about it is
// structurally wrong.
func TestVoltageComesThroughInKilovolts(t *testing.T) {
	got := ringBus(t)[0]

	want := map[string]float64{"V230": 230, "V66": 66}

	for _, level := range got.Definition.Levels {
		if level.NominalKV != want[level.Name] {
			t.Errorf("%s is %v kV, want %v", level.Name, level.NominalKV, want[level.Name])
		}
	}
}

// The whole point: a mapped substation is one the reduction can consume. This
// is the first time in the project that a node-breaker model has come from a
// real file format rather than from a fixture built by hand.
func TestAMappedSubstationReducesThroughTheStore(t *testing.T) {
	got := ringBus(t)[0]

	engine, err := topology.NewEngine(memory.New(), nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	site, err := engine.Define(t.Context(), got.Definition)
	if err != nil {
		t.Fatalf("Define: %v", err)
	}

	reduced, err := engine.Reduce(t.Context(), site.ID, nil)
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}

	// The 230 kV breaker joins N1 and N2, and the 66 kV isolator joins N3 and
	// N4, so four connectivity nodes collapse to two buses — one per voltage
	// level. Nodes never merge across a level, whatever a switch says, because
	// only a transformer connects two and a transformer has impedance.
	if len(reduced.Buses) != 2 {
		t.Errorf("got %d buses, want two — one per voltage level", len(reduced.Buses))
	}

	// And opening it splits them, which is the answer the node-breaker model
	// exists to be able to give.
	breaker := firstBreaker(t, got)

	split, err := engine.Reduce(t.Context(), site.ID, topology.Overrides{breaker: false})
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}

	if len(split.Buses) != 3 {
		t.Errorf("opening the 230 kV breaker gave %d buses, want three", len(split.Buses))
	}

	// And the transformer survives the reduction as a branch, which is what
	// makes it a transformer rather than a bus merge.
	if len(reduced.Branches) != 1 {
		t.Errorf("got %d branches, want the transformer", len(reduced.Branches))
	}
}

func firstBreaker(t *testing.T, result mapping.Result) id.ID {
	t.Helper()

	for _, item := range result.Definition.Equipment {
		if item.Kind == reduce.KindBreaker {
			return item.ID
		}
	}

	t.Fatal("the mapped substation has no breaker")

	return id.Nil
}

// SCL describes the substation as built, never as operated: it says where the
// breakers are, not where they currently sit. Anything but normally-closed
// would make every imported substation report islands that are not there.
func TestBreakersImportAsNormallyClosed(t *testing.T) {
	for _, item := range ringBus(t)[0].Definition.Equipment {
		if item.Kind.Switchable() && !item.NormallyClosed {
			t.Errorf("%s imported open", item.Name)
		}
	}
}

// An import must be able to find what it already wrote, by the name the source
// used. Without aliases a re-import cannot recognize anything.
func TestEverySCLNameIsRecordedAsAnAlias(t *testing.T) {
	got := ringBus(t)[0]

	values := map[string]bool{}
	for _, a := range got.Definition.Aliases {
		if a.Namespace != mapping.Namespace {
			t.Errorf("alias %q is in namespace %q", a.Value, a.Namespace)
		}

		values[a.Value] = true
	}

	for _, want := range []string{"RINGBUS", "RINGBUS/V230", "CB1", "TR1"} {
		if !values[want] {
			t.Errorf("no alias recorded for %q", want)
		}
	}
}

// A terminal naming a node the file does not define is a broken
// cross-reference, and importing it would produce a substation whose reduction
// reports islands that are not there.
func TestADanglingTerminalIsRefused(t *testing.T) {
	broken := `<?xml version="1.0"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL">
  <Header id="broken"/>
  <Substation name="S">
    <VoltageLevel name="V"><Voltage unit="V" multiplier="k">230</Voltage>
      <Bay name="B">
        <ConnectivityNode name="N1" pathName="S/V/B/N1"/>
        <ConductingEquipment name="CB1" type="CBR">
          <Terminal name="T1" connectivityNode="S/V/B/NOWHERE"/>
        </ConductingEquipment>
      </Bay>
    </VoltageLevel>
  </Substation>
</SCL>`

	parsed, err := parse.Decode(strings.NewReader(broken))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if _, err := mapping.Substations(parsed); err == nil {
		t.Fatal("a dangling terminal reference was accepted")
	}
}

// An ICD has no substation section. Mapping it produces nothing, which is a
// valid outcome rather than an error.
func TestAnICDMapsToNoSubstations(t *testing.T) {
	icd := `<SCL xmlns="http://www.iec.ch/61850/2003/SCL"><Header id="t"/>
  <IED name="T"><AccessPoint name="A"><Server><LDevice inst="P"/></Server></AccessPoint></IED>
</SCL>`

	parsed, err := parse.Decode(strings.NewReader(icd))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	got, err := mapping.Substations(parsed)
	if err != nil {
		t.Fatalf("Substations: %v", err)
	}

	if len(got) != 0 {
		t.Errorf("an ICD produced %d substations", len(got))
	}
}
