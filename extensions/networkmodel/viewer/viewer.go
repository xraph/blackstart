// Package viewer is the payload the 3D viewer draws.
//
// Parallel arrays rather than a list of objects, because that is what a
// renderer wants: a 2000-bus network is hundreds of thousands of vertices, and
// an array of objects costs an allocation per vertex at parse time and a copy
// into a typed array before upload.
//
// Deliberately dumb. No nesting, no references to resolve, no identifiers to
// join on beyond the ones a click needs to report. Anything cleverer would be
// a query language, and the platform already has an API for that.
package viewer

import (
	"errors"
	"fmt"

	"github.com/xraph/blackstart/extensions/networkmodel/canonical"
)

// Model is the whole payload.
type Model struct {
	// ModelID identifies the stored model these coordinates came from.
	ModelID string `json:"modelId"`

	// ContentHash lets a view be cited. A screenshot of a network nobody can
	// reproduce is not evidence of anything.
	ContentHash string `json:"contentHash"`

	Buses    Buses    `json:"buses"`
	Branches Branches `json:"branches"`
}

// Buses are drawn as instanced points.
type Buses struct {
	IDs   []string `json:"ids"`
	Names []string `json:"names"`

	// Coordinates are longitude, latitude and elevation triples in degrees and
	// meters — not ECEF. The conversion happens in the browser so the payload
	// stays readable and diffable, and so a fix to the geodetic maths does not
	// require re-exporting every model.
	Coordinates []float64 `json:"coordinates"`

	NominalKV []float64 `json:"nominalKv"`
}

// Branches are drawn as instanced line segments.
type Branches struct {
	IDs []string `json:"ids"`

	// From and To index into the bus arrays.
	From []int `json:"from"`
	To   []int `json:"to"`

	InService []bool `json:"inService"`
}

// Placement supplies where each bus is.
//
// A separate input rather than a field on the canonical model, because the
// canonical model is what the solver hashes and geometry is deliberately
// excluded from that hash — a substation moving fifty meters must not
// invalidate every result computed for the network.
type Placement map[string]Coordinate

// Coordinate is one bus's position.
type Coordinate struct {
	Lon, Lat, Elevation float64
}

// Build turns a canonical model and a set of placements into a payload.
//
// # Errors
//
// Returns an error when a branch names a bus the model does not contain. That
// is unreachable from a parsed case — the importer already checks it — and is
// checked again because this is the last point before a renderer, where the
// same mistake becomes NaN vertices and a line that fails to draw silently.
func Build(modelID, contentHash string, model canonical.Model, at Placement) (Model, error) {
	if len(model.Buses) == 0 {
		return Model{}, errors.New("viewer: the model has no buses")
	}

	out := Model{
		ModelID:     modelID,
		ContentHash: contentHash,
		Buses: Buses{
			IDs:         make([]string, 0, len(model.Buses)),
			Names:       make([]string, 0, len(model.Buses)),
			Coordinates: make([]float64, 0, len(model.Buses)*3),
			NominalKV:   make([]float64, 0, len(model.Buses)),
		},
		Branches: Branches{
			IDs:       make([]string, 0, len(model.Branches)),
			From:      make([]int, 0, len(model.Branches)),
			To:        make([]int, 0, len(model.Branches)),
			InService: make([]bool, 0, len(model.Branches)),
		},
	}

	// The model-level base, used only for a bus that states no nominal voltage
	// of its own. It used to be used for every bus, which drew a mixed-voltage
	// network as though it were all one level — the canonical form can say
	// otherwise now, so this is a fallback rather than the answer.
	fallbackKV := model.VBaseV / 1e3

	index := make(map[string]int, len(model.Buses))

	for position, bus := range model.Buses {
		index[bus.ID] = position

		out.Buses.IDs = append(out.Buses.IDs, bus.ID)
		out.Buses.Names = append(out.Buses.Names, bus.ID)

		// Non-positive means the bus states none, not that it sits at zero
		// volts — a bus drawn at zero would be colored as though it were a
		// voltage level of its own.
		nominalKV := fallbackKV
		if bus.VNominalV > 0 {
			nominalKV = bus.VNominalV / 1e3
		}

		out.Buses.NominalKV = append(out.Buses.NominalKV, nominalKV)

		// A bus with no placement sits at the origin off the coast of Africa,
		// which is conspicuous rather than invisible. Dropping it instead
		// would renumber every bus after it and silently move every branch.
		where := at[bus.ID]

		out.Buses.Coordinates = append(
			out.Buses.Coordinates, where.Lon, where.Lat, where.Elevation)
	}

	for _, branch := range model.Branches {
		from, ok := index[branch.FromID]
		if !ok {
			return Model{}, fmt.Errorf(
				"viewer: branch %s runs from %s, which is not a bus in this model",
				branch.ID, branch.FromID)
		}

		to, ok := index[branch.ToID]
		if !ok {
			return Model{}, fmt.Errorf(
				"viewer: branch %s runs to %s, which is not a bus in this model",
				branch.ID, branch.ToID)
		}

		out.Branches.IDs = append(out.Branches.IDs, branch.ID)
		out.Branches.From = append(out.Branches.From, from)
		out.Branches.To = append(out.Branches.To, to)
		out.Branches.InService = append(out.Branches.InService, branch.InService)
	}

	return out, nil
}
