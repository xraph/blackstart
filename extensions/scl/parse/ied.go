package parse

// IED is one intelligent electronic device — a relay, a controller, a meter.
type IED struct {
	Name          string        `xml:"name,attr"`
	Type          string        `xml:"type,attr"`
	Manufacturer  string        `xml:"manufacturer,attr"`
	ConfigVersion string        `xml:"configVersion,attr"`
	Description   string        `xml:"desc,attr"`
	AccessPoints  []AccessPoint `xml:"AccessPoint"`
}

// AccessPoint is one communication interface of an IED.
type AccessPoint struct {
	Name   string  `xml:"name,attr"`
	Server *Server `xml:"Server"`
}

// Server holds the logical devices an access point exposes.
type Server struct {
	LogicalDevices []LogicalDevice `xml:"LDevice"`
}

// LogicalDevice groups logical nodes.
type LogicalDevice struct {
	Inst string `xml:"inst,attr"`

	// LN0 is the mandatory logical node zero, which carries the datasets and
	// control blocks for the whole device. Separate from LNs because SCL gives
	// it its own element, not because it is a different kind of thing.
	LN0 *LogicalNode0 `xml:"LN0"`

	LNs []LogicalNode `xml:"LN"`
}

// LogicalNode0 is the device-level logical node.
type LogicalNode0 struct {
	LNClass  string          `xml:"lnClass,attr"`
	LNType   string          `xml:"lnType,attr"`
	Inst     string          `xml:"inst,attr"`
	DataSets []DataSet       `xml:"DataSet"`
	Reports  []ReportControl `xml:"ReportControl"`
	GSE      []GSEControl    `xml:"GSEControl"`
	Inputs   *Inputs         `xml:"Inputs"`
}

// LogicalNode is one function inside a logical device.
type LogicalNode struct {
	Prefix      string  `xml:"prefix,attr"`
	LNClass     string  `xml:"lnClass,attr"`
	Inst        string  `xml:"inst,attr"`
	LNType      string  `xml:"lnType,attr"`
	Description string  `xml:"desc,attr"`
	Inputs      *Inputs `xml:"Inputs"`
}

// DataSet is a named collection of data attributes.
type DataSet struct {
	Name        string `xml:"name,attr"`
	Description string `xml:"desc,attr"`
	FCDA        []FCDA `xml:"FCDA"`
}

// FCDA is one functionally-constrained data attribute in a dataset.
type FCDA struct {
	LDInst  string `xml:"ldInst,attr"`
	Prefix  string `xml:"prefix,attr"`
	LNClass string `xml:"lnClass,attr"`
	LNInst  string `xml:"lnInst,attr"`
	DOName  string `xml:"doName,attr"`
	DAName  string `xml:"daName,attr"`
	FC      string `xml:"fc,attr"`
}

// ReportControl configures a reporting service.
type ReportControl struct {
	Name     string `xml:"name,attr"`
	DataSet  string `xml:"datSet,attr"`
	RptID    string `xml:"rptID,attr"`
	Buffered bool   `xml:"buffered,attr"`
	ConfRev  int    `xml:"confRev,attr"`
}

// GSEControl configures a GOOSE publication.
type GSEControl struct {
	Name    string `xml:"name,attr"`
	DataSet string `xml:"datSet,attr"`
	AppID   string `xml:"appID,attr"`
	ConfRev int    `xml:"confRev,attr"`
	Type    string `xml:"type,attr"`
}

// Inputs holds the external references a logical node subscribes to.
//
// The communication graph, and the reason the SCL importer is worth its
// weight: ExtRef says which IED listens to which other IED's published data.
// That is a second graph over the same substation, independent of the
// electrical one, and it is not derivable from the topology.
type Inputs struct {
	ExtRefs []ExtRef `xml:"ExtRef"`
}

// ExtRef is one subscription to another device's data.
type ExtRef struct {
	IEDName string `xml:"iedName,attr"`
	LDInst  string `xml:"ldInst,attr"`
	Prefix  string `xml:"prefix,attr"`
	LNClass string `xml:"lnClass,attr"`
	LNInst  string `xml:"lnInst,attr"`
	DOName  string `xml:"doName,attr"`
	DAName  string `xml:"daName,attr"`

	// ServiceType is GOOSE, SMV or Report. Empty in older files, where the
	// binding is implied by what the source publishes.
	ServiceType string `xml:"serviceType,attr"`

	// SrcCBName names the publishing control block, which is what makes an
	// ExtRef resolvable to a specific GOOSE stream rather than merely to a
	// device.
	SrcCBName  string `xml:"srcCBName,attr"`
	SrcLDInst  string `xml:"srcLDInst,attr"`
	SrcPrefix  string `xml:"srcPrefix,attr"`
	SrcLNClass string `xml:"srcLNClass,attr"`
	SrcLNInst  string `xml:"srcLNInst,attr"`
}

// Communication carries the network addressing for every published stream.
type Communication struct {
	SubNetworks []SubNetwork `xml:"SubNetwork"`
}

// SubNetwork is one network segment.
type SubNetwork struct {
	Name         string        `xml:"name,attr"`
	Type         string        `xml:"type,attr"`
	ConnectedAPs []ConnectedAP `xml:"ConnectedAP"`
}

// ConnectedAP attaches an IED's access point to a subnetwork.
type ConnectedAP struct {
	IEDName string   `xml:"iedName,attr"`
	APName  string   `xml:"apName,attr"`
	Address *Address `xml:"Address"`
	GSEs    []GSE    `xml:"GSE"`
}

// Address is a set of protocol parameters.
type Address struct {
	P []P `xml:"P"`
}

// P is one addressing parameter, typed by its attribute rather than by its
// element name — which is why this cannot be a struct of named fields.
type P struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

// GSE is the network address of one GOOSE control block.
//
// Joined to the GSEControl by (iedName, ldInst, cbName). SCL splits them
// deliberately: the control block says what is published, and this says where
// — and the two are configured by different people at different times.
type GSE struct {
	LDInst  string   `xml:"ldInst,attr"`
	CBName  string   `xml:"cbName,attr"`
	Address *Address `xml:"Address"`
	MinTime *TimeVal `xml:"MinTime"`
	MaxTime *TimeVal `xml:"MaxTime"`
}

// TimeVal is a duration with an SI multiplier.
type TimeVal struct {
	Unit       string  `xml:"unit,attr"`
	Multiplier string  `xml:"multiplier,attr"`
	Value      float64 `xml:",chardata"`
}
