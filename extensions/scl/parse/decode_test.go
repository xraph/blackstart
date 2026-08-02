package parse_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/scl/parse"
)

func ringBus(t *testing.T) *parse.SCL {
	t.Helper()

	file, err := os.Open("testdata/ring.scd")
	if err != nil {
		t.Fatalf("opening the fixture: %v", err)
	}
	defer func() { _ = file.Close() }()

	got, err := parse.Decode(file)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	return got
}

// The substation section is what the topology importer reads, and the
// structure it needs is the nesting: site, level, bay, node, equipment,
// terminal.
func TestTheSubstationSectionParses(t *testing.T) {
	got := ringBus(t)

	if len(got.Substations) != 1 || got.Substations[0].Name != "RINGBUS" {
		t.Fatalf("substations: %+v", got.Substations)
	}

	level := got.Substations[0].VoltageLevels[0]

	if len(level.Bays) != 1 || len(level.Bays[0].ConnectivityNodes) != 2 {
		t.Fatalf("bays: %+v", level.Bays)
	}

	if len(level.Bays[0].Equipment) != 3 {
		t.Errorf("got %d pieces of equipment, want three", len(level.Bays[0].Equipment))
	}

	// A transformer sits at the voltage level rather than in a bay, because it
	// joins two levels and belongs to neither exclusively. A parser that only
	// looked inside bays would silently lose every transformer.
	if len(level.PowerTransformers) != 1 {
		t.Errorf("the level's transformer was not read: %+v", level.PowerTransformers)
	}
}

// The multiplier is the whole reason Voltage is a struct. Reading the value
// alone turns a 230 kV substation into a 230 volt one, which is a factor of a
// thousand that still parses.
func TestTheVoltageMultiplierIsKept(t *testing.T) {
	level := ringBus(t).Substations[0].VoltageLevels[0]

	if level.Voltage == nil {
		t.Fatal("the voltage element was not read")
	}

	if level.Voltage.Value != 230 || level.Voltage.Multiplier != "k" {
		t.Errorf("voltage is %v%s%s", level.Voltage.Value,
			level.Voltage.Multiplier, level.Voltage.Unit)
	}
}

// Terminals reference connectivity nodes by path name, not by bare name: a
// bare name is only unique within its bay, which is why SCL requires the path.
func TestTerminalsCarryTheFullNodePath(t *testing.T) {
	breaker := ringBus(t).Substations[0].VoltageLevels[0].Bays[0].Equipment[0]

	if breaker.Type != "CBR" {
		t.Fatalf("the first device is a %s", breaker.Type)
	}

	if len(breaker.Terminals) != 2 {
		t.Fatalf("a breaker with %d terminals", len(breaker.Terminals))
	}

	for _, terminal := range breaker.Terminals {
		if !strings.HasPrefix(terminal.ConnectivityNode, "RINGBUS/V230/BAY1/") {
			t.Errorf("terminal %s references %q", terminal.Name, terminal.ConnectivityNode)
		}
	}
}

// LNode is the only thing joining the electrical world to the communication
// one. Losing it means importing two unrelated models from one file.
func TestEquipmentBindsToALogicalNode(t *testing.T) {
	breaker := ringBus(t).Substations[0].VoltageLevels[0].Bays[0].Equipment[0]

	if len(breaker.LNodes) != 1 {
		t.Fatalf("the breaker has %d logical-node bindings", len(breaker.LNodes))
	}

	if got := breaker.LNodes[0]; got.IEDName != "IED1" || got.LNClass != "CSWI" {
		t.Errorf("binding is %+v", got)
	}
}

// The subscription graph is the reason the SCL importer is worth its weight: a
// second graph over the same substation, independent of the electrical one and
// not derivable from it.
func TestGooseSubscriptionsAreRead(t *testing.T) {
	device := ringBus(t).IEDs[0].AccessPoints[0].Server.LogicalDevices[0]

	if device.LN0 == nil || len(device.LN0.GSE) != 1 {
		t.Fatalf("the published control block was not read: %+v", device.LN0)
	}

	if got := device.LN0.GSE[0]; got.Name != "gcbTrip" || got.DataSet != "dsTrip" {
		t.Errorf("control block is %+v", got)
	}

	if len(device.LNs) != 1 || device.LNs[0].Inputs == nil {
		t.Fatalf("the subscribing node was not read: %+v", device.LNs)
	}

	subscription := device.LNs[0].Inputs.ExtRefs[0]

	// It names another IED, which is what makes it an edge rather than an
	// annotation.
	if subscription.IEDName != "IED2" || subscription.SrcCBName != "gcbProt" {
		t.Errorf("subscription is %+v", subscription)
	}
}

// The control block says what is published and the Communication section says
// where. They are joined by (iedName, ldInst, cbName) and are configured by
// different people at different times, which is why SCL splits them.
func TestTheGooseAddressIsReadFromTheCommunicationSection(t *testing.T) {
	got := ringBus(t)

	if got.Communication == nil {
		t.Fatal("the communication section was not read")
	}

	// Found by name rather than by position. Access points arrive in file
	// order and a fixture gains devices over time, so an index would tie this
	// test to the order rather than to the thing it checks.
	var point *parse.ConnectedAP

	for index, candidate := range got.Communication.SubNetworks[0].ConnectedAPs {
		if candidate.IEDName == "IED1" {
			point = &got.Communication.SubNetworks[0].ConnectedAPs[index]
		}
	}

	if point == nil {
		t.Fatal("IED1 has no connected access point")
	}

	if len(point.GSEs) != 1 || point.GSEs[0].CBName != "gcbTrip" {
		t.Fatalf("GSE is %+v", point.GSEs)
	}

	byType := map[string]string{}
	for _, p := range point.GSEs[0].Address.P {
		byType[p.Type] = p.Value
	}

	// Typed by attribute rather than by element name, which is why Address
	// cannot be a struct of named fields.
	if byType["MAC-Address"] != "01-0C-CD-01-00-01" || byType["APPID"] != "0001" {
		t.Errorf("address parameters are %v", byType)
	}
}

// A schema-defined format has no legitimate use for a DOCTYPE. Its presence
// means the file is either not an SCL file or came from something worth
// knowing about before trusting it.
func TestADoctypeIsRefused(t *testing.T) {
	hostile := `<?xml version="1.0"?>
<!DOCTYPE SCL [<!ENTITY xxe SYSTEM "file:///etc/passwd">]>
<SCL><Header id="x"/></SCL>`

	if _, err := parse.Decode(strings.NewReader(hostile)); !errors.Is(err, parse.ErrDoctype) {
		t.Errorf("got %v, want ErrDoctype", err)
	}
}

// Unbounded nesting recurses. The bound is far above any real file and far
// below what would exhaust a stack.
func TestDeepNestingIsRefused(t *testing.T) {
	var deep strings.Builder

	deep.WriteString(`<SCL>`)

	for range 200 {
		deep.WriteString(`<Bay>`)
	}

	for range 200 {
		deep.WriteString(`</Bay>`)
	}

	deep.WriteString(`</SCL>`)

	_, err := parse.Decode(strings.NewReader(deep.String()))
	if err == nil {
		t.Fatal("a document nested 200 deep was accepted")
	}

	if !strings.Contains(err.Error(), "nest") {
		t.Errorf("the message does not say what was wrong: %v", err)
	}
}

func TestSomethingThatIsNotSCLIsRefused(t *testing.T) {
	for name, document := range map[string]string{
		"another root": `<Substation name="X"/>`,
		"not XML":      `{"substation": "X"}`,
		"empty":        ``,
		"truncated":    `<SCL><Header id="x"`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parse.Decode(strings.NewReader(document)); err == nil {
				t.Errorf("%s was accepted", name)
			}
		})
	}
}

// An ICD file has no Substation section at all — it describes a device type
// before it is bound to any plant. That is a first-class path, not an error.
func TestAnICDWithNoSubstationIsValid(t *testing.T) {
	icd := `<?xml version="1.0"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL">
  <Header id="device-template"/>
  <IED name="TEMPLATE" type="RELAY" manufacturer="Nobody">
    <AccessPoint name="AP1"><Server><LDevice inst="PROT">
      <LN0 lnClass="LLN0" lnType="LLN0_0" inst=""/>
    </LDevice></Server></AccessPoint>
  </IED>
</SCL>`

	got, err := parse.Decode(strings.NewReader(icd))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if len(got.Substations) != 0 {
		t.Errorf("an ICD produced %d substations", len(got.Substations))
	}

	if len(got.IEDs) != 1 {
		t.Fatalf("the device was not read")
	}
}
