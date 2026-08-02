package matpower

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/xraph/blackstart/extensions/networkmodel/canonical"
)

// ParserVersion identifies this reader in a provenance record.
//
// A dataset is only as reproducible as the code that interpreted it: the same
// bytes mean different things to different parsers, so a fix here changes what
// a file says and the version has to move with it.
const ParserVersion = "matpower/0.1.0"

// MATPOWER column indices, spelled out rather than sliced by number at the use
// site. A silently shifted column produces a plausible network, which is the
// most expensive kind of wrong this package can produce.
const (
	busI, busType, busPD, busQD, busGS, busBS = 0, 1, 2, 3, 4, 5
	busVM, busVA, busBaseKV                   = 7, 8, 9

	genBus, genPG, genQG, genQMax, genQMin, genVG = 0, 1, 2, 3, 4, 5
	genStatus                                     = 7

	branchF, branchT, branchR, branchX, branchB = 0, 1, 2, 3, 4
	branchRateA, branchTap, branchShift         = 5, 8, 9
	branchStatus                                = 10
)

// MATPOWER bus types.
const (
	typePQ  = 1
	typePV  = 2
	typeRef = 3
)

// Case is a parsed MATPOWER file.
type Case struct {
	// Name is what the case calls itself, when it says.
	Name string

	// BaseMVA is the system apparent-power base.
	BaseMVA float64

	// Model is the network in canonical form, ready to hash or to send.
	Model canonical.Model

	// Aliases maps each entity's blackstart-side identifier stub to the
	// identifier MATPOWER used, so an import never discards the source's own
	// name. Keyed by the canonical identifier this parser assigned.
	Aliases map[string]string
}

// Parse reads a MATPOWER case file.
//
// Everything is converted to per unit against the file's own baseMVA, because
// that is what the canonical model carries and what the solver computes with.
// Voltages stay in per unit as MATPOWER already gives them.
//
// # Errors
//
// Returns an error naming the missing or malformed field. A case that cannot
// be read is better than one read wrong: a network with a transposed column
// still solves and still produces numbers.
func Parse(source string) (Case, error) {
	doc, err := lex(source)
	if err != nil {
		return Case{}, err
	}

	baseMVA, ok := doc.scalars["baseMVA"]
	if !ok {
		return Case{}, errors.New(
			"matpower: the case has no mpc.baseMVA; every per-unit value in it " +
				"is meaningless without one")
	}

	if baseMVA.value <= 0 {
		return Case{}, fmt.Errorf(
			"matpower: line %d: mpc.baseMVA is %v; a base must be positive",
			baseMVA.line, baseMVA.value)
	}

	busRows, err := requireMatrix(doc, "bus", 10)
	if err != nil {
		return Case{}, err
	}

	branchRows, err := requireMatrix(doc, "branch", 11)
	if err != nil {
		return Case{}, err
	}

	// Generators are optional: a case with none is a load-only network, which
	// is unusual but not malformed.
	genRows := doc.matrices["gen"].rows

	out := Case{
		BaseMVA: baseMVA.value,
		Aliases: map[string]string{},
		Model: canonical.Model{
			SBaseVA: baseMVA.value * 1e6,
		},
	}

	// A case names itself in `mpc.name` when it names itself at all, and that
	// is a quoted string rather than a number.
	if name, ok := doc.texts["name"]; ok {
		out.Name = name.value
	}

	generation, limits, scheduled := aggregateGenerators(genRows, baseMVA.value)

	buses, byNumber, err := buildBuses(busRows, baseMVA.value, generation, limits, scheduled, out.Aliases)
	if err != nil {
		return Case{}, err
	}

	out.Model.Buses = buses

	// The voltage base comes from the first bus that states one. MATPOWER
	// carries a per-bus base and the canonical model carries one for the whole
	// network, which is a real narrowing — recorded here rather than hidden,
	// because a network spanning several voltages has more than one and the
	// canonical form currently cannot say so.
	out.Model.VBaseV = firstVoltageBase(busRows)

	branches, err := buildBranches(branchRows, baseMVA.value, byNumber, out.Aliases)
	if err != nil {
		return Case{}, err
	}

	out.Model.Branches = branches

	return out, nil
}

// requireMatrix fetches a matrix and checks its width.
func requireMatrix(doc document, name string, columns int) ([][]float64, error) {
	m, ok := doc.matrices[name]
	if !ok {
		return nil, fmt.Errorf("matpower: the case has no mpc.%s", name)
	}

	for index, row := range m.rows {
		if len(row) < columns {
			return nil, fmt.Errorf(
				"matpower: line %d: mpc.%s row %d has %d columns; %d are needed, "+
					"and a short row means the columns after it would be read from "+
					"the wrong place",
				m.line, name, index+1, len(row), columns)
		}
	}

	return m.rows, nil
}

// aggregateGenerators sums generation and limits onto their buses.
//
// Several machines can sit on one bus, and the canonical model has one
// injection per bus. Summing is what MATPOWER itself does.
func aggregateGenerators(
	rows [][]float64, baseMVA float64,
) (map[int]float64, map[int][2]float64, map[int]float64) {
	generation := map[int]float64{}
	limits := map[int][2]float64{}
	scheduled := map[int]float64{}

	for _, row := range rows {
		if len(row) <= genStatus || row[genStatus] <= 0 {
			continue
		}

		bus := int(row[genBus])
		generation[bus] += row[genPG] / baseMVA

		lo, hi := row[genQMin], row[genQMax]
		if !math.IsInf(lo, 0) && !math.IsInf(hi, 0) {
			current := limits[bus]
			limits[bus] = [2]float64{current[0] + lo/baseMVA, current[1] + hi/baseMVA}
		}

		if vg := row[genVG]; vg > 0 {
			scheduled[bus] = vg
		}
	}

	return generation, limits, scheduled
}

// buildBuses turns bus rows into canonical buses.
func buildBuses(
	rows [][]float64,
	baseMVA float64,
	generation map[int]float64,
	limits map[int][2]float64,
	scheduled map[int]float64,
	aliases map[string]string,
) ([]canonical.Bus, map[int]string, error) {
	buses := make([]canonical.Bus, 0, len(rows))
	byNumber := make(map[int]string, len(rows))

	for index, row := range rows {
		number := int(row[busI])

		kind := uint8(row[busType])
		if kind != typePQ && kind != typePV && kind != typeRef {
			return nil, nil, fmt.Errorf(
				"matpower: mpc.bus row %d: bus %d has type %d; 1, 2 and 3 are the "+
					"only types this reader knows", index+1, number, kind)
		}

		// Identifiers are derived from the MATPOWER bus number rather than
		// minted, so re-parsing the same file produces the same canonical model
		// and therefore the same content hash. A minted identifier would give a
		// different hash on every import of an unchanged file.
		identifier := "bus-" + strconv.Itoa(number)

		if _, seen := byNumber[number]; seen {
			return nil, nil, fmt.Errorf(
				"matpower: mpc.bus row %d: bus %d appears twice", index+1, number)
		}

		byNumber[number] = identifier
		aliases[identifier] = strconv.Itoa(number)

		bus := canonical.Bus{
			ID:         identifier,
			Kind:       kind,
			PInjection: generation[number] - row[busPD]/baseMVA,
			QInjection: -row[busQD] / baseMVA,
			VMagnitude: row[busVM],
			VAngle:     row[busVA] * math.Pi / 180,
			GShunt:     row[busGS] / baseMVA,
			BShunt:     row[busBS] / baseMVA,
			QMin:       math.Inf(-1),
			QMax:       math.Inf(1),
		}

		// The slack's injection is whatever the network requires, so it is left
		// at zero rather than seeded from the file's own answer — which would
		// make the imported case carry a solution it has not yet computed.
		if kind == typeRef {
			bus.PInjection = 0
		}

		if kind == typePV || kind == typeRef {
			if v, ok := scheduled[number]; ok {
				bus.VMagnitude = v
			}
		}

		if kind == typePV {
			if pair, ok := limits[number]; ok {
				bus.QMin, bus.QMax = pair[0], pair[1]
			}
		}

		buses = append(buses, bus)
	}

	return buses, byNumber, nil
}

// buildBranches turns branch rows into canonical branches.
func buildBranches(
	rows [][]float64, baseMVA float64, byNumber map[int]string, aliases map[string]string,
) ([]canonical.Branch, error) {
	branches := make([]canonical.Branch, 0, len(rows))

	for index, row := range rows {
		from, to := int(row[branchF]), int(row[branchT])

		fromID, ok := byNumber[from]
		if !ok {
			return nil, fmt.Errorf(
				"matpower: mpc.branch row %d: from-bus %d is not in mpc.bus",
				index+1, from)
		}

		toID, ok := byNumber[to]
		if !ok {
			return nil, fmt.Errorf(
				"matpower: mpc.branch row %d: to-bus %d is not in mpc.bus",
				index+1, to)
		}

		// Derived from the row's position, so re-parsing an unchanged file
		// produces the same identifiers and the same hash. MATPOWER branches
		// have no identifier of their own.
		identifier := "branch-" + strconv.Itoa(index)
		aliases[identifier] = fmt.Sprintf("%d-%d", from, to)

		// MATPOWER writes 0 for "no transformer"; 1.0 is the identity, and a
		// zero ratio would divide the admittance by zero.
		tap := row[branchTap]
		if tap <= 0 {
			tap = 1
		}

		branches = append(branches, canonical.Branch{
			ID:     identifier,
			FromID: fromID,
			ToID:   toID,
			R:      row[branchR],
			X:      row[branchX],
			B:      row[branchB],
			// Against the case's own base, like every other conversion here.
			// This divided by a literal 100 until it was noticed: correct on
			// the overwhelming majority of MATPOWER cases, which state a
			// 100 MVA base, and silently wrong on any that do not.
			Rating:    row[branchRateA] / baseMVA,
			InService: row[branchStatus] > 0,
			Tap:       tap,
			Shift:     row[branchShift] * math.Pi / 180,
		})
	}

	return branches, nil
}

// firstVoltageBase reads the voltage base from the first bus that states one.
//
// A real narrowing: MATPOWER carries a base per bus and the canonical model
// carries one for the network. A case spanning several voltages has more than
// one, and the canonical form currently cannot say so — recorded here rather
// than hidden, because silently taking the first is the kind of assumption
// that surfaces as a factor-of-two error in a report.
func firstVoltageBase(rows [][]float64) float64 {
	for _, row := range rows {
		if len(row) > busBaseKV && row[busBaseKV] > 0 {
			return row[busBaseKV] * 1e3
		}
	}

	// Nothing stated. 1.0 volt rather than zero, so a per-unit conversion
	// divides by something and the resulting numbers are visibly wrong rather
	// than infinite.
	return 1
}

// AliasNamespace is the namespace MATPOWER identifiers are recorded under.
//
// Namespaced because two sources will use the same string for different
// things: "3" is a bus number here and could be anything elsewhere.
const AliasNamespace = "matpower"

// LooksLikeCase reports whether text is plausibly a MATPOWER case.
//
// Used by an importer choosing a parser. Deliberately cheap and deliberately
// not a validation: it answers "should I try", and Parse answers "is it".
func LooksLikeCase(text string) bool {
	return strings.Contains(text, "mpc.baseMVA")
}
