// Package mapping turns a parsed SCL file into blackstart's topology.
//
// Pure, like the parser: an AST in, a topology definition out. Nothing here
// touches a store, so "does this file describe the substation I think it
// does" is a question a test can answer.
//
// This is where Phase 1's ConnectivityNode entity earns its place. SCL's
// connectivity nodes map one-to-one onto blackstart's, because both model the
// same thing — an electrical point that breakers join. Had the network model
// been bus-branch, every file would need a reduction applied at import and the
// node-breaker detail would be gone before anything could use it.
package mapping

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xraph/blackstart/extensions/scl/parse"
	"github.com/xraph/blackstart/extensions/topology"
	tid "github.com/xraph/blackstart/extensions/topology/id"
	"github.com/xraph/blackstart/extensions/topology/network"
	"github.com/xraph/blackstart/extensions/topology/reduce"
	"github.com/xraph/blackstart/pkg/id"
)

// Namespace is what SCL identifiers are recorded under.
const Namespace = "iec61850"

// Kinds maps IEC 61850-6 equipment abbreviations onto blackstart's vocabulary.
//
// Only the types that are electrical topology. A current or voltage
// transformer — CTR, VTR — is an asset that measures the network without being
// part of it: it carries no load flow, and modelling one as a branch would put
// an impedance in the network that is not there.
var Kinds = map[string]reduce.Kind{
	"CBR": reduce.KindBreaker,
	"DIS": reduce.KindSwitch,
	"PTR": reduce.KindTransformer,
	"GEN": reduce.KindGenerator,
	"CAB": reduce.KindLine,
	"LIN": reduce.KindLine,
	"IFL": reduce.KindLine,
	"MOT": reduce.KindLoad,
}

// Result is what one substation in a file mapped to.
type Result struct {
	// Definition is the substation, ready to write.
	Definition topology.Definition

	// Skipped names equipment the mapping did not model, with the reason.
	// Reported rather than dropped silently: an operator comparing a count
	// against their own tooling needs to know what was left out and why.
	Skipped []Skip
}

// Skip records one piece of equipment that was not mapped.
type Skip struct {
	Name   string
	Type   string
	Reason string
}

// Substations maps every substation in a file.
//
// # Errors
//
// Returns an error when a terminal names a connectivity node the file does not
// define. That is a broken cross-reference, and importing it would produce a
// substation whose reduction reports islands that do not exist.
func Substations(file *parse.SCL) ([]Result, error) {
	if file == nil {
		return nil, nil
	}

	// Connectivity nodes are collected across the whole file before any
	// equipment is mapped. A feeder between two substations in one SCD has a
	// terminal in each, and mapping substations independently would leave both
	// ends unresolvable — reporting a broken cross-reference for a file that
	// is entirely correct.
	nodes := map[string]id.ID{}
	declared := make([]*Result, 0, len(file.Substations))

	for _, site := range file.Substations {
		mapped := skeleton(site, nodes)
		declared = append(declared, mapped)
	}

	out := make([]Result, 0, len(file.Substations))

	for index, site := range file.Substations {
		if err := populate(declared[index], site, nodes); err != nil {
			return nil, err
		}

		out = append(out, *declared[index])
	}

	return out, nil
}

// skeleton records a substation, its levels and its connectivity nodes.
//
// Split from populate so every node in the file exists before any terminal is
// resolved against it.
func skeleton(site parse.Substation, nodes map[string]id.ID) *Result {
	siteID := tid.NewSubstation()

	out := &Result{
		Definition: topology.Definition{
			Substation: network.Substation{
				ID:   siteID,
				Name: site.Name,
				Code: site.Name,
			},
			Aliases: []network.ExternalAlias{alias(siteID, site.Name)},
		},
	}

	for _, level := range site.VoltageLevels {
		levelID := tid.NewVoltageLevel()

		out.Definition.Levels = append(out.Definition.Levels, network.VoltageLevel{
			ID:           levelID,
			SubstationID: siteID,
			NominalKV:    kilovolts(level.Voltage),
			Name:         level.Name,
		})

		out.Definition.Aliases = append(out.Definition.Aliases,
			alias(levelID, site.Name+"/"+level.Name))

		for _, bay := range level.Bays {
			addNodes(out, bay, site.Name, level.Name, levelID, nodes)
		}
	}

	return out
}

// populate maps a substation's equipment, resolving terminals against every
// node in the file.
func populate(out *Result, site parse.Substation, nodes map[string]id.ID) error {
	for index, level := range site.VoltageLevels {
		levelID := out.Definition.Levels[index].ID

		for _, bay := range level.Bays {
			addEquipment(out, bay.Equipment, levelID, nodes)

			for _, transformer := range bay.PowerTransformers {
				addTransformer(out, transformer, levelID, nodes)
			}
		}

		// Transformers at the level rather than in a bay. A transformer joins
		// two levels and belongs to neither exclusively, so SCL allows it
		// here — and a mapper that only walked bays would lose every one.
		for _, transformer := range level.PowerTransformers {
			addTransformer(out, transformer, levelID, nodes)
		}
	}

	return check(out, nodes)
}

// addNodes records a bay's connectivity nodes.
func addNodes(
	out *Result, bay parse.Bay, site, level string, levelID id.ID, nodes map[string]id.ID,
) {
	bayID := tid.NewBay()

	for _, node := range bay.ConnectivityNodes {
		nodeID := tid.NewConnectivityNode()

		// The path name as written when there is one, and a reconstructed path
		// when there is not. Older files omit it, and terminals in those files
		// reference the bay-qualified name instead.
		path := node.PathName
		if path == "" {
			path = strings.Join([]string{site, level, bay.Name, node.Name}, "/")
		}

		nodes[path] = nodeID
		nodes[bay.Name+"/"+node.Name] = nodeID

		out.Definition.Nodes = append(out.Definition.Nodes, network.ConnectivityNode{
			ID:             nodeID,
			VoltageLevelID: levelID,
			BayID:          bayID,
			Name:           node.Name,
		})

		out.Definition.Aliases = append(out.Definition.Aliases, alias(nodeID, path))
	}
}

// addEquipment records a bay's conducting equipment and its terminals.
func addEquipment(
	out *Result, equipment []parse.ConductingEquipment, levelID id.ID, nodes map[string]id.ID,
) {
	for _, item := range equipment {
		kind, known := Kinds[strings.ToUpper(item.Type)]
		if !known {
			out.Skipped = append(out.Skipped, Skip{
				Name: item.Name,
				Type: item.Type,
				Reason: "not electrical topology; it carries no load flow, and " +
					"modelling it as a branch would put an impedance in the " +
					"network that is not there",
			})

			continue
		}

		equipmentID := tid.NewEquipment()

		out.Definition.Equipment = append(out.Definition.Equipment, network.Equipment{
			ID:             equipmentID,
			Kind:           kind,
			VoltageLevelID: levelID,
			Name:           item.Name,
			// SCL describes the substation as built, not as operated: it says
			// where the breakers are, never where they currently sit. Normally
			// closed is the only defensible import default, and it is what
			// every study without a live feed assumes.
			NormallyClosed: true,
		})

		out.Definition.Aliases = append(out.Definition.Aliases, alias(equipmentID, item.Name))

		addTerminals(out, equipmentID, item.Terminals, nodes)
	}
}

// addTransformer records a transformer and the terminals of its windings.
func addTransformer(
	out *Result, transformer parse.PowerTransformer, levelID id.ID, nodes map[string]id.ID,
) {
	equipmentID := tid.NewEquipment()

	out.Definition.Equipment = append(out.Definition.Equipment, network.Equipment{
		ID:             equipmentID,
		Kind:           reduce.KindTransformer,
		VoltageLevelID: levelID,
		Name:           transformer.Name,
	})

	out.Definition.Aliases = append(out.Definition.Aliases, alias(equipmentID, transformer.Name))

	// Windings in file order, and their terminals in order within each. That
	// order is what makes "the high side" well defined, and a transformer with
	// its windings swapped still solves — as a different machine.
	for _, winding := range transformer.Windings {
		addTerminals(out, equipmentID, winding.Terminals, nodes)
	}
}

// addTerminals records one device's terminals, numbered in file order.
func addTerminals(
	out *Result, equipmentID id.ID, terminals []parse.Terminal, nodes map[string]id.ID,
) {
	for index, terminal := range terminals {
		nodeID, known := nodes[terminalPath(terminal)]
		if !known {
			// Left unresolved here and reported by checkTerminals, which sees
			// the whole file. A terminal can legitimately reference a node in
			// another bay that has not been walked yet.
			continue
		}

		out.Definition.Terminals = append(out.Definition.Terminals, network.Terminal{
			ID:                 tid.NewTerminal(),
			EquipmentID:        equipmentID,
			ConnectivityNodeID: nodeID,
			Sequence:           index,
		})
	}
}

// terminalPath is the key a terminal resolves against.
func terminalPath(terminal parse.Terminal) string {
	if terminal.ConnectivityNode != "" {
		return terminal.ConnectivityNode
	}

	return terminal.BayName + "/" + terminal.CNodeName
}

// check removes devices whose terminals did not all resolve, and reports why.
//
// Two different situations land here and they are told apart by count. A
// device with no resolved terminals at all references nodes the file does not
// define — a broken cross-reference, and importing it would produce a
// substation whose reduction reports islands that are not there.
//
// A device with some but not enough is a boundary: a feeder whose far end is
// another substation, in another file. That is not an error and is extremely
// common — every substation has feeders leaving it — but it cannot stay in a
// single-substation reduction either, because a line with one end is not a
// branch of this substation's internal topology. It is dropped and reported,
// so the count an operator compares against their own tooling is explained.
func check(out *Result, nodes map[string]id.ID) error {
	if len(nodes) == 0 {
		// No connectivity nodes at all is an IED-only file, which is valid.
		return nil
	}

	attached := map[id.ID]int{}
	for _, terminal := range out.Definition.Terminals {
		attached[terminal.EquipmentID]++
	}

	var (
		dangling []string
		keep     []network.Equipment
		dropped  = map[id.ID]bool{}
	)

	for _, item := range out.Definition.Equipment {
		switch count := attached[item.ID]; {
		case count == 0:
			dangling = append(dangling, item.Name)

		case item.Kind.Impedance() && count < 2:
			out.Skipped = append(out.Skipped, Skip{
				Name: item.Name,
				Type: string(item.Kind),
				Reason: "only one end is inside this file; a feeder whose far end " +
					"is another substation is not a branch of this substation's " +
					"internal topology",
			})

			dropped[item.ID] = true

		default:
			keep = append(keep, item)
		}
	}

	if len(dangling) > 0 {
		sort.Strings(dangling)

		return fmt.Errorf(
			"scl: %s reference connectivity nodes this file does not define; "+
				"importing that would produce a substation whose reduction reports "+
				"islands that are not there", strings.Join(dangling, ", "))
	}

	out.Definition.Equipment = keep

	// The dropped devices' terminals go too. A terminal naming equipment that
	// is no longer there is exactly the dangling reference the store refuses.
	terminals := out.Definition.Terminals[:0]

	for _, terminal := range out.Definition.Terminals {
		if !dropped[terminal.EquipmentID] {
			terminals = append(terminals, terminal)
		}
	}

	out.Definition.Terminals = terminals

	return nil
}

// kilovolts converts an SCL voltage to kilovolts.
//
// The multiplier is why this exists. SCL writes 400 kV as value 400 with
// multiplier "k", and reading the value alone gives a 400-volt substation —
// which the reduction accepts, because nothing about it is structurally wrong.
func kilovolts(voltage *parse.Voltage) float64 {
	if voltage == nil {
		return 0
	}

	switch voltage.Multiplier {
	case "k":
		return voltage.Value
	case "M":
		return voltage.Value * 1e3
	case "m":
		return voltage.Value * 1e-6
	case "":
		return voltage.Value / 1e3
	default:
		return 0
	}
}

// alias records what SCL called an entity.
func alias(entity id.ID, value string) network.ExternalAlias {
	return network.ExternalAlias{
		ID:        tid.NewExternalAlias(),
		EntityID:  entity,
		Namespace: Namespace,
		Value:     value,
	}
}
