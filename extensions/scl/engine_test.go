package scl_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/scl"
	sclmemory "github.com/xraph/blackstart/extensions/scl/store/memory"
	"github.com/xraph/blackstart/extensions/topology"
	topomemory "github.com/xraph/blackstart/extensions/topology/store/memory"
	"github.com/xraph/blackstart/pkg/bserr"
)

func engine(t *testing.T) *scl.Engine {
	t.Helper()

	// A real topology engine over its own in-memory store, not a fake. What is
	// under test is that one file writes two models, and a fake topology would
	// let a broken substation through unnoticed.
	topo, err := topology.NewEngine(topomemory.New(), nil)
	if err != nil {
		t.Fatalf("topology.NewEngine: %v", err)
	}

	e, err := scl.NewEngine(sclmemory.New(), topo, nil, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	return e
}

func fixture(t *testing.T) []byte {
	t.Helper()

	source, err := os.ReadFile("parse/testdata/ring.scd")
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}

	return source
}

// The claim the extension exists to support: one file, two models, one import.
func TestOneFileWritesBothModels(t *testing.T) {
	e := engine(t)

	got, err := e.Import(t.Context(), scl.ImportRequest{Source: fixture(t)})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if len(got.Substations) != 1 {
		t.Errorf("got %d substations", len(got.Substations))
	}

	if got.Devices != 2 || got.Publications != 2 || got.Subscriptions != 1 {
		t.Errorf("wrote %d devices, %d publications, %d subscriptions",
			got.Devices, got.Publications, got.Subscriptions)
	}

	// And the electrical model is usable, not merely written: four
	// connectivity nodes reduce to two buses, one per voltage level.
	reduced, err := e.Topology().Reduce(t.Context(), got.Substations[0], nil)
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}

	if len(reduced.Buses) != 2 {
		t.Errorf("the imported substation reduced to %d buses, want two",
			len(reduced.Buses))
	}
}

// The question the electrical model cannot answer. Asked from both ends,
// because they are different questions and a store that answered one by
// scanning for the other would look correct in a single-edge test.
func TestSubscriptionsAreReadableFromBothEnds(t *testing.T) {
	e := engine(t)

	if _, err := e.Import(t.Context(), scl.ImportRequest{Source: fixture(t)}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	subscriber, err := e.Device(t.Context(), "IED1")
	if err != nil {
		t.Fatalf("Device: %v", err)
	}

	publisher, err := e.Device(t.Context(), "IED2")
	if err != nil {
		t.Fatalf("Device: %v", err)
	}

	// IED1 listens to IED2.
	listens, err := e.Listens(t.Context(), subscriber.ID)
	if err != nil {
		t.Fatalf("Listens: %v", err)
	}

	if len(listens) != 1 || listens[0].PublisherID != publisher.ID {
		t.Errorf("IED1 listens to %+v", listens)
	}

	// And IED2 is listened to by IED1 — the direction that matters when
	// deciding whether losing a device is consequential.
	listeners, err := e.Listeners(t.Context(), publisher.ID)
	if err != nil {
		t.Fatalf("Listeners: %v", err)
	}

	if len(listeners) != 1 || listeners[0].SubscriberID != subscriber.ID {
		t.Errorf("IED2 is listened to by %+v", listeners)
	}

	// Neither device answers the other's question, which a store scanning the
	// wrong column would get wrong in exactly one direction.
	if empty, _ := e.Listens(t.Context(), publisher.ID); len(empty) != 0 {
		t.Errorf("IED2 reported listening to %+v", empty)
	}
}

// Re-importing a corrected file is how a configuration is updated. A second
// identifier for the same device would orphan every subscription pointing at
// the first.
func TestReimportingKeepsDeviceIdentifiers(t *testing.T) {
	e := engine(t)
	source := fixture(t)

	if _, err := e.Import(t.Context(), scl.ImportRequest{Source: source}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	first, err := e.Device(t.Context(), "IED1")
	if err != nil {
		t.Fatalf("Device: %v", err)
	}

	if _, err := e.Import(t.Context(), scl.ImportRequest{Source: source}); err != nil {
		t.Fatalf("re-Import: %v", err)
	}

	second, err := e.Device(t.Context(), "IED1")
	if err != nil {
		t.Fatalf("Device: %v", err)
	}

	if first.ID != second.ID {
		t.Errorf("re-importing minted a new identifier: %s then %s", first.ID, second.ID)
	}

	devices, err := e.Devices(t.Context(), 0)
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}

	if len(devices) != 2 {
		t.Errorf("two imports of one file produced %d devices", len(devices))
	}
}

// The publications carry their network address, which is the join SCL leaves
// to the reader.
func TestAPublicationKeepsItsWireAddress(t *testing.T) {
	e := engine(t)

	if _, err := e.Import(t.Context(), scl.ImportRequest{Source: fixture(t)}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	found, err := e.Device(t.Context(), "IED1")
	if err != nil {
		t.Fatalf("Device: %v", err)
	}

	published, err := e.Publications(t.Context(), found.ID)
	if err != nil {
		t.Fatalf("Publications: %v", err)
	}

	if len(published) != 1 {
		t.Fatalf("IED1 publishes %d control blocks", len(published))
	}

	if published[0].MAC != "01-0C-CD-01-00-01" || published[0].NetworkAppID != "0001" {
		t.Errorf("publication is %+v", published[0])
	}
}

// An ICD binds to no plant, so importing one needs no topology at all.
func TestAnICDImportsWithNoTopologyEngine(t *testing.T) {
	e, err := scl.NewEngine(sclmemory.New(), nil, nil, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	icd := `<SCL xmlns="http://www.iec.ch/61850/2003/SCL"><Header id="t"/>
  <IED name="TEMPLATE" type="RELAY" manufacturer="Nobody">
    <AccessPoint name="AP"><Server><LDevice inst="PROT">
      <LN0 lnClass="LLN0" inst=""><GSEControl name="gcb" datSet="ds"/></LN0>
    </LDevice></Server></AccessPoint>
  </IED></SCL>`

	got, err := e.Import(t.Context(), scl.ImportRequest{Source: []byte(icd)})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if len(got.Substations) != 0 || got.Devices != 1 || got.Publications != 1 {
		t.Errorf("result is %+v", got)
	}
}

// A file that does describe substations and has nowhere to put them is refused
// rather than having its electrical model silently discarded.
func TestASubstationFileNeedsATopologyEngine(t *testing.T) {
	e, err := scl.NewEngine(sclmemory.New(), nil, nil, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	_, err = e.Import(t.Context(), scl.ImportRequest{Source: fixture(t)})
	if err == nil {
		t.Fatal("a file with substations imported with no topology engine")
	}

	if !strings.Contains(err.Error(), "silently discarded") {
		t.Errorf("the message does not say what would be lost: %v", err)
	}
}

// A subscription whose publisher is configured elsewhere is counted, not
// written: an edge needs both ends, and inventing a node for a device this
// file does not describe would put something in the graph nothing else knows
// about.
func TestAnUnresolvedSubscriptionIsCountedNotWritten(t *testing.T) {
	e, err := scl.NewEngine(sclmemory.New(), nil, nil, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	partial := `<SCL xmlns="http://www.iec.ch/61850/2003/SCL"><Header id="p"/>
  <IED name="IED1"><AccessPoint name="AP"><Server><LDevice inst="CTRL">
    <LN0 lnClass="LLN0" inst=""/>
    <LN lnClass="CSWI" inst="1">
      <Inputs><ExtRef iedName="ELSEWHERE" doName="Op" daName="general" srcCBName="x"/></Inputs>
    </LN>
  </LDevice></Server></AccessPoint></IED></SCL>`

	got, err := e.Import(t.Context(), scl.ImportRequest{Source: []byte(partial)})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if got.Subscriptions != 0 || got.Unresolved != 1 {
		t.Errorf("wrote %d subscriptions and counted %d unresolved",
			got.Subscriptions, got.Unresolved)
	}
}

func TestAnEngineNeedsAStore(t *testing.T) {
	if _, err := scl.NewEngine(nil, nil, nil, nil); err == nil {
		t.Error("NewEngine accepted a nil store")
	}
}

func TestAMissingDeviceIsNotFound(t *testing.T) {
	e := engine(t)

	if _, err := e.Device(t.Context(), "NOBODY"); !errors.Is(err, bserr.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestNothingToImportIsRefused(t *testing.T) {
	if _, err := engine(t).Import(t.Context(), scl.ImportRequest{}); err == nil {
		t.Error("an empty import was accepted")
	}
}
