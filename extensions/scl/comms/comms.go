// Package comms is the communication graph an SCL file describes.
//
// Pure, like the rest of the SCL reader. Devices publish GOOSE streams and
// subscribe to each other's, and that is a second graph over the same
// substation — independent of the electrical one and not derivable from it.
// Two relays on the same bus may exchange nothing; two relays in different
// substations may be tightly coupled.
//
// It is the strongest thing the SCL importer produces. A protection engineer
// asking "if this IED is lost, whose logic stops working" is asking a
// question the electrical model cannot answer at all.
package comms

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xraph/blackstart/extensions/scl/parse"
)

// Publication is one GOOSE control block with its network address.
//
// SCL keeps the two apart on purpose: the control block says what is
// published, the Communication section says where, and the two are configured
// by different people at different times. Joining them is this package's job
// so nothing downstream has to know they were ever separate.
type Publication struct {
	// IEDName, LDInst and CBName are the key SCL joins on.
	IEDName string
	LDInst  string
	CBName  string

	// AppID is the application identifier the control block declares.
	AppID string

	// DataSet names what is published.
	DataSet string

	// ConfRev changes whenever the dataset's contents change. A subscriber
	// configured against an older revision is receiving a different set of
	// values than it expects, which is a real misconfiguration and is
	// invisible on the wire.
	ConfRev int

	// MAC, VLAN and NetworkAppID come from the Communication section. Empty
	// when the file declares a control block with no address — a valid state
	// in an ICD, and a misconfiguration in an SCD.
	MAC          string
	VLAN         string
	NetworkAppID string
}

// Addressed reports whether the publication has somewhere to go.
func (p Publication) Addressed() bool { return p.MAC != "" }

// Subscription is one device listening to another's published data.
type Subscription struct {
	// SubscriberIED is the device that listens.
	SubscriberIED string

	// PublisherIED is the device it listens to.
	PublisherIED string

	// SubscriberPath identifies the logical node holding the ExtRef.
	SubscriberPath string

	// PublisherCB names the publishing control block when the file says.
	// Older files omit it, and the binding is then implied by what the source
	// publishes — resolvable only when the publisher has exactly one.
	PublisherCB string

	// Service is GOOSE, SMV or Report.
	Service string

	// Data is the object and attribute subscribed to.
	Data string

	// Resolved says whether the publisher and its control block were both
	// found in this file. An SCD may reference devices configured elsewhere,
	// so an unresolved subscription is a real state rather than an error —
	// but one nothing should treat as a confirmed edge.
	Resolved bool
}

// Graph is every publication and subscription in a file.
type Graph struct {
	Publications  []Publication
	Subscriptions []Subscription
}

// Build reads the communication graph.
//
// # Errors
//
// None. Every inconsistency an SCL file can express here — a subscription to a
// device that is not in the file, a control block with no address — is a real
// state that a partial configuration legitimately has. They are reported on
// the records themselves rather than raised, because refusing the file would
// make an incomplete SCD unimportable, and an incomplete SCD is the normal
// state of a substation mid-commissioning.
func Build(file *parse.SCL) Graph {
	if file == nil {
		return Graph{}
	}

	out := Graph{
		Publications:  publications(file),
		Subscriptions: subscriptions(file),
	}

	known := map[string]int{}
	for _, publication := range out.Publications {
		known[publication.IEDName]++
		known[publication.IEDName+"/"+publication.CBName]++
	}

	for index, subscription := range out.Subscriptions {
		out.Subscriptions[index].Resolved = resolves(subscription, known)
	}

	sort.SliceStable(out.Subscriptions, func(left, right int) bool {
		return key(out.Subscriptions[left]) < key(out.Subscriptions[right])
	})

	sort.SliceStable(out.Publications, func(left, right int) bool {
		return out.Publications[left].IEDName+"/"+out.Publications[left].CBName <
			out.Publications[right].IEDName+"/"+out.Publications[right].CBName
	})

	return out
}

// resolves reports whether a subscription's target is in this file.
func resolves(subscription Subscription, known map[string]int) bool {
	if subscription.PublisherIED == "" {
		return false
	}

	if subscription.PublisherCB != "" {
		return known[subscription.PublisherIED+"/"+subscription.PublisherCB] > 0
	}

	// No control block named. Resolvable only when the publisher has exactly
	// one, because with two there is no way to tell which stream carries the
	// data — and guessing would produce an edge that looks confirmed.
	return known[subscription.PublisherIED] == 1
}

// key orders subscriptions deterministically, so a golden built from one file
// does not move between runs.
func key(s Subscription) string {
	return s.SubscriberIED + "\x00" + s.SubscriberPath + "\x00" +
		s.PublisherIED + "\x00" + s.PublisherCB + "\x00" + s.Data
}

// publications collects every GOOSE control block, joined to its address.
func publications(file *parse.SCL) []Publication {
	addresses := gseAddresses(file)

	var out []Publication

	for _, ied := range file.IEDs {
		for _, point := range ied.AccessPoints {
			if point.Server == nil {
				continue
			}

			for _, device := range point.Server.LogicalDevices {
				if device.LN0 == nil {
					continue
				}

				for _, control := range device.LN0.GSE {
					publication := Publication{
						IEDName: ied.Name,
						LDInst:  device.Inst,
						CBName:  control.Name,
						AppID:   control.AppID,
						DataSet: control.DataSet,
						ConfRev: control.ConfRev,
					}

					if found, ok := addresses[ied.Name+"\x00"+device.Inst+"\x00"+control.Name]; ok {
						publication.MAC = found["MAC-Address"]
						publication.VLAN = found["VLAN-ID"]
						publication.NetworkAppID = found["APPID"]
					}

					out = append(out, publication)
				}
			}
		}
	}

	return out
}

// gseAddresses indexes the Communication section by the key SCL joins on.
func gseAddresses(file *parse.SCL) map[string]map[string]string {
	out := map[string]map[string]string{}

	if file.Communication == nil {
		return out
	}

	for _, network := range file.Communication.SubNetworks {
		for _, point := range network.ConnectedAPs {
			for _, gse := range point.GSEs {
				if gse.Address == nil {
					continue
				}

				parameters := map[string]string{}
				for _, p := range gse.Address.P {
					parameters[p.Type] = strings.TrimSpace(p.Value)
				}

				out[point.IEDName+"\x00"+gse.LDInst+"\x00"+gse.CBName] = parameters
			}
		}
	}

	return out
}

// subscriptions collects every external reference.
func subscriptions(file *parse.SCL) []Subscription {
	var out []Subscription

	for _, ied := range file.IEDs {
		for _, point := range ied.AccessPoints {
			if point.Server == nil {
				continue
			}

			for _, device := range point.Server.LogicalDevices {
				if device.LN0 != nil && device.LN0.Inputs != nil {
					out = append(out, from(ied.Name, device.Inst, "LLN0",
						device.LN0.Inputs.ExtRefs)...)
				}

				for _, node := range device.LNs {
					if node.Inputs == nil {
						continue
					}

					out = append(out, from(ied.Name, device.Inst,
						node.Prefix+node.LNClass+node.Inst, node.Inputs.ExtRefs)...)
				}
			}
		}
	}

	return out
}

// from turns one logical node's external references into subscriptions.
func from(ied, device, node string, refs []parse.ExtRef) []Subscription {
	out := make([]Subscription, 0, len(refs))

	for _, ref := range refs {
		// A reference naming no source device is an unbound input: a slot the
		// engineer left for later. Common in a partially commissioned file and
		// not an edge, so it is skipped rather than recorded as a
		// subscription to nothing.
		if ref.IEDName == "" {
			continue
		}

		out = append(out, Subscription{
			SubscriberIED:  ied,
			PublisherIED:   ref.IEDName,
			SubscriberPath: device + "/" + node,
			PublisherCB:    ref.SrcCBName,
			Service:        service(ref),
			Data:           fmt.Sprintf("%s.%s", ref.DOName, ref.DAName),
		})
	}

	return out
}

// service reports what kind of stream a reference subscribes to.
//
// Older files omit serviceType entirely. GOOSE is the right assumption there:
// it is what ExtRef was introduced for, and a report subscription names a
// control block that a GOOSE binding never would.
func service(ref parse.ExtRef) string {
	if ref.ServiceType != "" {
		return ref.ServiceType
	}

	return "GOOSE"
}
