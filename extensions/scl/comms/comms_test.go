package comms_test

import (
	"os"
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/scl/comms"
	"github.com/xraph/blackstart/extensions/scl/parse"
)

func graph(t *testing.T) comms.Graph {
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

	return comms.Build(parsed)
}

// SCL keeps the control block and its address apart on purpose — what is
// published and where it goes are configured by different people at different
// times. Joining them is this package's job so nothing downstream has to know
// they were ever separate.
func TestAPublicationCarriesItsNetworkAddress(t *testing.T) {
	got := graph(t)

	if len(got.Publications) != 2 {
		t.Fatalf("got %d publications: %+v", len(got.Publications), got.Publications)
	}

	byName := map[string]comms.Publication{}
	for _, publication := range got.Publications {
		byName[publication.IEDName+"/"+publication.CBName] = publication
	}

	trip, ok := byName["IED1/gcbTrip"]
	if !ok {
		t.Fatalf("IED1/gcbTrip is missing from %v", byName)
	}

	if trip.MAC != "01-0C-CD-01-00-01" || trip.VLAN != "005" {
		t.Errorf("address is %+v", trip)
	}

	// The application identifier appears twice in SCL and they are different
	// things: the control block's is a name, the address's is the number on
	// the wire. Conflating them would make two streams indistinguishable to a
	// capture tool.
	if trip.AppID != "TRIP1" || trip.NetworkAppID != "0001" {
		t.Errorf("appID %q and network appID %q", trip.AppID, trip.NetworkAppID)
	}

	if !trip.Addressed() {
		t.Error("a publication with a MAC reported itself unaddressed")
	}
}

// The edge the electrical model cannot express: which device listens to which
// other device. A protection engineer asking "if this IED is lost, whose logic
// stops working" is asking a question topology cannot answer at all.
func TestASubscriptionIsAnEdgeBetweenDevices(t *testing.T) {
	got := graph(t)

	if len(got.Subscriptions) != 1 {
		t.Fatalf("got %d subscriptions: %+v", len(got.Subscriptions), got.Subscriptions)
	}

	subscription := got.Subscriptions[0]

	if subscription.SubscriberIED != "IED1" || subscription.PublisherIED != "IED2" {
		t.Errorf("subscription runs %s → %s",
			subscription.SubscriberIED, subscription.PublisherIED)
	}

	// Both ends are in this file, so the edge is confirmed rather than
	// merely asserted by the subscriber.
	if !subscription.Resolved {
		t.Error("a subscription whose publisher is in the file is unresolved")
	}

	if subscription.Service != "GOOSE" || !strings.HasPrefix(subscription.Data, "Op.") {
		t.Errorf("subscription is %+v", subscription)
	}
}

// An SCD may reference devices configured elsewhere. That is a real state, not
// an error — refusing the file would make an incomplete SCD unimportable, and
// incomplete is the normal state of a substation mid-commissioning.
func TestASubscriptionToADeviceOutsideTheFileIsUnresolved(t *testing.T) {
	partial := `<SCL xmlns="http://www.iec.ch/61850/2003/SCL"><Header id="p"/>
  <IED name="IED1"><AccessPoint name="AP"><Server><LDevice inst="CTRL">
    <LN0 lnClass="LLN0" inst=""/>
    <LN lnClass="CSWI" inst="1">
      <Inputs><ExtRef iedName="ELSEWHERE" ldInst="P" lnClass="PTOC" lnInst="1"
                      doName="Op" daName="general" srcCBName="gcbX"/></Inputs>
    </LN>
  </LDevice></Server></AccessPoint></IED>
</SCL>`

	parsed, err := parse.Decode(strings.NewReader(partial))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	got := comms.Build(parsed)

	if len(got.Subscriptions) != 1 {
		t.Fatalf("got %d subscriptions", len(got.Subscriptions))
	}

	if got.Subscriptions[0].Resolved {
		t.Error("a subscription to a device outside the file reported itself resolved")
	}
}

// A reference naming no source device is a slot the engineer left for later —
// common in a partly commissioned file, and not an edge to anywhere.
func TestAnUnboundInputIsNotASubscription(t *testing.T) {
	unbound := `<SCL xmlns="http://www.iec.ch/61850/2003/SCL"><Header id="u"/>
  <IED name="IED1"><AccessPoint name="AP"><Server><LDevice inst="CTRL">
    <LN0 lnClass="LLN0" inst=""/>
    <LN lnClass="CSWI" inst="1">
      <Inputs><ExtRef doName="Op" daName="general"/></Inputs>
    </LN>
  </LDevice></Server></AccessPoint></IED>
</SCL>`

	parsed, err := parse.Decode(strings.NewReader(unbound))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if got := comms.Build(parsed); len(got.Subscriptions) != 0 {
		t.Errorf("an unbound input produced %+v", got.Subscriptions)
	}
}

// With no control block named, the binding is only resolvable when the
// publisher has exactly one stream. With two there is no way to tell which
// carries the data, and guessing would produce an edge that looks confirmed.
func TestAnAmbiguousBindingIsNotResolved(t *testing.T) {
	ambiguous := `<SCL xmlns="http://www.iec.ch/61850/2003/SCL"><Header id="a"/>
  <IED name="PUB"><AccessPoint name="AP"><Server><LDevice inst="P">
    <LN0 lnClass="LLN0" inst="">
      <GSEControl name="gcbA" datSet="dsA"/>
      <GSEControl name="gcbB" datSet="dsB"/>
    </LN0>
  </LDevice></Server></AccessPoint></IED>
  <IED name="SUB"><AccessPoint name="AP"><Server><LDevice inst="C">
    <LN0 lnClass="LLN0" inst=""/>
    <LN lnClass="CSWI" inst="1">
      <Inputs><ExtRef iedName="PUB" doName="Op" daName="general"/></Inputs>
    </LN>
  </LDevice></Server></AccessPoint></IED>
</SCL>`

	parsed, err := parse.Decode(strings.NewReader(ambiguous))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	got := comms.Build(parsed)

	if len(got.Subscriptions) != 1 || got.Subscriptions[0].Resolved {
		t.Errorf("an ambiguous binding resolved: %+v", got.Subscriptions)
	}

	// One named stream on the same publisher does resolve, which is what makes
	// the ambiguous case a real distinction rather than a blanket refusal.
	named := strings.Replace(ambiguous,
		`<ExtRef iedName="PUB" doName="Op"`,
		`<ExtRef iedName="PUB" srcCBName="gcbA" doName="Op"`, 1)

	parsed, err = parse.Decode(strings.NewReader(named))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if got := comms.Build(parsed); !got.Subscriptions[0].Resolved {
		t.Error("a subscription naming its control block did not resolve")
	}
}

// A control block with no address is valid in an ICD and a misconfiguration in
// an SCD. Reported on the record rather than raised, because this package
// cannot tell which kind of file it is reading.
func TestAnUnaddressedPublicationIsReportedNotRefused(t *testing.T) {
	icd := `<SCL xmlns="http://www.iec.ch/61850/2003/SCL"><Header id="i"/>
  <IED name="T"><AccessPoint name="AP"><Server><LDevice inst="P">
    <LN0 lnClass="LLN0" inst=""><GSEControl name="gcb" datSet="ds"/></LN0>
  </LDevice></Server></AccessPoint></IED>
</SCL>`

	parsed, err := parse.Decode(strings.NewReader(icd))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	got := comms.Build(parsed)

	if len(got.Publications) != 1 {
		t.Fatalf("got %d publications", len(got.Publications))
	}

	if got.Publications[0].Addressed() {
		t.Error("a control block with no address reported itself addressed")
	}
}
