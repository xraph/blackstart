// Package wire converts a stored network model into the solver's contract.
//
// One of exactly two places per-unit conversion happens in this system, and
// the only one on the way out. The canonical model is per unit because that is
// what a case file states and what the content hash covers; the wire contract
// is SI because a number crossing a process boundary with an implied base is a
// number that will eventually be interpreted against the wrong one.
//
// Pure, and deliberately so: getting a base wrong produces a network that
// solves and is wrong by a constant factor, which no downstream check catches.
// A test here can multiply by hand.
package wire

import (
	"errors"
	"fmt"
	"math"

	solverv1 "github.com/xraph/blackstart/proto/gen/go/blackstart/solver/v1"

	"github.com/xraph/blackstart/extensions/networkmodel/canonical"
)

// Model converts a canonical model to the solver's wire form.
//
// # Conversions
//
// Power is per unit times the apparent-power base, in watts and VAr. Voltage
// is per unit times the voltage base, in volts. Impedance is per unit times
// the base impedance V²/S, in ohms; admittance is per unit divided by it, in
// siemens. Angles are radians on both sides and are not converted — the
// canonical model already holds radians, and the importer is where degrees
// were left behind.
//
// # Errors
//
// Returns an error when the base is missing or non-positive, and when a branch
// names a bus the model does not contain. A zero base would divide every
// impedance by zero; a missing bus would send an index the solver reads as
// another bus entirely.
func Model(name string, in canonical.Model) (*solverv1.NetworkModel, error) {
	if in.SBaseVA <= 0 || math.IsNaN(in.SBaseVA) {
		return nil, fmt.Errorf(
			"wire: apparent-power base is %v VA; every per-unit value in this "+
				"model is meaningless without a positive one", in.SBaseVA)
	}

	if in.VBaseV <= 0 || math.IsNaN(in.VBaseV) {
		return nil, fmt.Errorf(
			"wire: voltage base is %v V; impedance cannot be converted without "+
				"a positive one", in.VBaseV)
	}

	if len(in.Buses) == 0 {
		return nil, errors.New("wire: a model with no buses describes nothing")
	}

	// The base impedance. Every per-unit impedance multiplies by this and
	// every per-unit admittance divides by it, which is the whole of the
	// conversion and the single thing most worth getting right.
	zBase := in.VBaseV * in.VBaseV / in.SBaseVA

	out := &solverv1.NetworkModel{
		Name:     name,
		SBaseVa:  in.SBaseVA,
		VBaseV:   in.VBaseV,
		Buses:    make([]*solverv1.Bus, 0, len(in.Buses)),
		Branches: make([]*solverv1.Branch, 0, len(in.Branches)),
	}

	index := make(map[string]uint32, len(in.Buses))

	for position, bus := range in.Buses {
		index[bus.ID] = uint32(position)

		out.Buses = append(out.Buses, busOf(bus, in.SBaseVA, in.VBaseV, zBase))
	}

	for _, branch := range in.Branches {
		converted, err := branchOf(branch, index, in.SBaseVA, zBase)
		if err != nil {
			return nil, err
		}

		out.Branches = append(out.Branches, converted)
	}

	return out, nil
}

// busOf converts one bus.
func busOf(bus canonical.Bus, sBase, vBase, zBase float64) *solverv1.Bus {
	out := &solverv1.Bus{
		Id:            bus.ID,
		Kind:          kindOf(bus.Kind),
		PInjectionW:   bus.PInjection * sBase,
		QInjectionVar: bus.QInjection * sBase,
		VMagnitudeV:   bus.VMagnitude * vBase,
		// Radians on both sides. The importer is where degrees were left
		// behind, and converting again here would be a factor of 57 that still
		// solves.
		VAngleRad: bus.VAngle,
		// Shunt admittance divides by the base impedance rather than
		// multiplying: an admittance is the reciprocal of an impedance, and
		// multiplying would be wrong by zBase squared — about 10^8 on a
		// transmission network, which is conspicuous, and about 1 on a
		// hypothetical unit-base network, which is not.
		GShuntS: bus.GShunt / zBase,
		BShuntS: bus.BShunt / zBase,
	}

	// Volts on both sides, so this one is copied rather than converted. It is
	// the single field of a bus that was already SI in the canonical model,
	// and multiplying it by vBase — which every line above does — would put a
	// 138 kV bus at 19 gigavolts.
	//
	// Absent stays absent, and what counts as absent is the bus's own
	// judgement rather than a comparison spelled here. Sending a zero — or an
	// infinity, which this used to let through — rather than nothing would be
	// read downstream as a bus at that voltage: a rating of nothing, or one
	// that cannot be encoded at all.
	if nominal, stated := bus.StatedNominalV(); stated {
		out.VNominalV = &nominal
	}

	// Absent means unlimited, which is not zero. Infinities are how the
	// canonical model spells "no stated limit", and sending them as finite
	// numbers would pin a generator that has none.
	if !math.IsInf(bus.QMin, -1) {
		limit := bus.QMin * sBase
		out.QMinVar = &limit
	}

	if !math.IsInf(bus.QMax, 1) {
		limit := bus.QMax * sBase
		out.QMaxVar = &limit
	}

	return out
}

// branchOf converts one branch, resolving its bus references to indices.
func branchOf(
	branch canonical.Branch, index map[string]uint32, sBase, zBase float64,
) (*solverv1.Branch, error) {
	from, ok := index[branch.FromID]
	if !ok {
		return nil, fmt.Errorf(
			"wire: branch %q runs from %q, which is not a bus in this model",
			branch.ID, branch.FromID)
	}

	to, ok := index[branch.ToID]
	if !ok {
		return nil, fmt.Errorf(
			"wire: branch %q runs to %q, which is not a bus in this model",
			branch.ID, branch.ToID)
	}

	return &solverv1.Branch{
		Id:      branch.ID,
		FromBus: from,
		ToBus:   to,
		ROhm:    branch.R * zBase,
		XOhm:    branch.X * zBase,
		BShuntS: branch.B / zBase,
		// Zero stays zero. An unrated branch has an unknown limit rather than
		// a limit of nothing, and the contract says so — multiplying does not
		// change that, but it is worth stating that nothing here invents one.
		RatingVa:  branch.Rating * sBase,
		InService: branch.InService,
		Tap:       branch.Tap,
		ShiftRad:  branch.Shift,
	}, nil
}

// kindOf maps a canonical bus type onto the wire enumeration.
func kindOf(kind uint8) solverv1.BusKind {
	switch kind {
	case 2:
		return solverv1.BusKind_BUS_KIND_PV
	case 3:
		return solverv1.BusKind_BUS_KIND_SLACK
	default:
		return solverv1.BusKind_BUS_KIND_PQ
	}
}
