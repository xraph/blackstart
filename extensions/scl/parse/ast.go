// Package parse reads IEC 61850-6 substation configuration files.
//
// Pure: bytes in, an abstract syntax tree out. No store, no context, no
// network. The mapping onto blackstart's topology lives elsewhere, so a
// question about what a file says can be answered without a database.
//
// Written from the IEC 61850-6 Ed 2.1 element definitions rather than
// generated from the schema. The XSD is not redistributable, and vendoring it
// into an Apache-2.0 repository is a licensing question this project does not
// need to answer — so the structs are hand-written and only cover the elements
// blackstart maps. An element this does not model is skipped, not rejected: a
// real SCD carries a great deal that a topology importer has no use for.
package parse

import "encoding/xml"

// SCL is the root element of every configuration file.
type SCL struct {
	XMLName xml.Name `xml:"SCL"`

	Header        Header         `xml:"Header"`
	Substations   []Substation   `xml:"Substation"`
	Communication *Communication `xml:"Communication"`
	IEDs          []IED          `xml:"IED"`
}

// Header identifies the file.
type Header struct {
	ID       string `xml:"id,attr"`
	Version  string `xml:"version,attr"`
	Revision string `xml:"revision,attr"`
	ToolID   string `xml:"toolID,attr"`
}

// Substation is a physical site.
type Substation struct {
	Name          string         `xml:"name,attr"`
	Description   string         `xml:"desc,attr"`
	VoltageLevels []VoltageLevel `xml:"VoltageLevel"`
}

// VoltageLevel is one nominal voltage within a substation.
type VoltageLevel struct {
	Name        string   `xml:"name,attr"`
	Description string   `xml:"desc,attr"`
	Voltage     *Voltage `xml:"Voltage"`
	Bays        []Bay    `xml:"Bay"`

	// PowerTransformers sit at the voltage level as well as in a bay, because
	// a transformer joins two levels and belongs to neither exclusively.
	PowerTransformers []PowerTransformer `xml:"PowerTransformer"`
}

// Voltage is a nominal voltage with an SI multiplier.
//
// The multiplier is the whole reason this is a struct rather than a float: SCL
// writes 400 kV as value 400 with multiplier "k", and reading the value alone
// gives a 400-volt substation.
type Voltage struct {
	Multiplier string  `xml:"multiplier,attr"`
	Unit       string  `xml:"unit,attr"`
	Value      float64 `xml:",chardata"`
}

// Bay is a container for one circuit position.
type Bay struct {
	Name              string                `xml:"name,attr"`
	Description       string                `xml:"desc,attr"`
	ConnectivityNodes []ConnectivityNode    `xml:"ConnectivityNode"`
	Equipment         []ConductingEquipment `xml:"ConductingEquipment"`
	PowerTransformers []PowerTransformer    `xml:"PowerTransformer"`
	Functions         []Function            `xml:"Function"`
}

// ConnectivityNode is an electrical point.
type ConnectivityNode struct {
	Name string `xml:"name,attr"`

	// PathName is the file-wide unique path to this node, and it is what
	// terminals reference. SCL requires it precisely because a bare name is
	// only unique within its bay.
	PathName    string `xml:"pathName,attr"`
	Description string `xml:"desc,attr"`
}

// ConductingEquipment is one device with electrical function.
type ConductingEquipment struct {
	Name string `xml:"name,attr"`

	// Type is the IEC 61850-6 abbreviation: CBR, DIS, PTR, CTR, VTR and the
	// rest. Kept as written rather than mapped here, so the AST says what the
	// file said and the mapping is one testable step elsewhere.
	Type        string     `xml:"type,attr"`
	Description string     `xml:"desc,attr"`
	Terminals   []Terminal `xml:"Terminal"`
	LNodes      []LNode    `xml:"LNode"`
}

// PowerTransformer joins voltage levels.
type PowerTransformer struct {
	Name        string               `xml:"name,attr"`
	Type        string               `xml:"type,attr"`
	Description string               `xml:"desc,attr"`
	Windings    []TransformerWinding `xml:"TransformerWinding"`
	LNodes      []LNode              `xml:"LNode"`
}

// TransformerWinding is one side of a transformer.
type TransformerWinding struct {
	Name      string     `xml:"name,attr"`
	Type      string     `xml:"type,attr"`
	Terminals []Terminal `xml:"Terminal"`
}

// Terminal joins equipment to a connectivity node.
type Terminal struct {
	Name string `xml:"name,attr"`

	// ConnectivityNode is the path name of the node, not its bare name.
	ConnectivityNode string `xml:"connectivityNode,attr"`

	SubstationName   string `xml:"substationName,attr"`
	VoltageLevelName string `xml:"voltageLevelName,attr"`
	BayName          string `xml:"bayName,attr"`
	CNodeName        string `xml:"cNodeName,attr"`
}

// Function is a bay-level grouping of logical nodes.
type Function struct {
	Name   string  `xml:"name,attr"`
	LNodes []LNode `xml:"LNode"`
}

// LNode binds a piece of primary plant to a logical node inside an IED.
//
// The join between the electrical world and the communication one: the
// substation section says what the plant is, the IED section says what
// software runs on it, and this is the only thing connecting them.
type LNode struct {
	IEDName string `xml:"iedName,attr"`
	LDInst  string `xml:"ldInst,attr"`
	Prefix  string `xml:"prefix,attr"`
	LNClass string `xml:"lnClass,attr"`
	LNInst  string `xml:"lnInst,attr"`
	LNType  string `xml:"lnType,attr"`
}
