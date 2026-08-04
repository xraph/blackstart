package shape

import (
	"fmt"
	"math"
)

// Resample places structures at a nominal spacing along a path that is already
// real.
//
// The difference from SyntheticRoute is what is being invented. SyntheticRoute
// invents the path as well as the structures: it draws a great circle between
// two endpoints and divides it. Resample keeps a digitised right-of-way exactly
// as surveyed and only invents where the structures stand along it — so a line
// that turns at an angle point still turns, and a line that crosses a ridge
// still crosses it.
//
// That matters because the path is what weather is sampled along. A great
// circle between two substations can miss the exposed ridge the real conductor
// runs over by tens of kilometres, and a gust sampled at the wrong place is a
// wrong answer no amount of per-span resolution fixes.
//
// The vertices of the input are preserved rather than resampled away. They are
// the turns the digitiser recorded, and dropping one to hit a uniform spacing
// would straighten a corner that is really there — trading a real feature for
// a tidy interval. Structures are therefore *at least* as dense as the input
// vertices, and the nominal spacing applies within each of its segments.
//
// The result is asset.Routed geometry, never asset.Surveyed: the route is real
// and the structures on it are not.
//
// # Errors
//
// Returns an error when the path is not a valid line or the nominal span is not
// positive.
func Resample(path Line, nominalSpanM float64) (Line, error) {
	if err := path.Validate(); err != nil {
		return nil, err
	}

	if nominalSpanM <= 0 || math.IsNaN(nominalSpanM) {
		return nil, fmt.Errorf(
			"shape: nominal span length %v m; a path cannot be divided by a "+
				"length that is not positive", nominalSpanM)
	}

	out := Line{path[0]}

	for index := 1; index < len(path); index++ {
		from, to := path[index-1], path[index]

		segment := from.DistanceTo(to)
		if segment <= 0 {
			// A repeated vertex. Digitised data has them, and a zero-length
			// span is not a thing that can fail, so it is dropped rather than
			// stored as a degenerate span with no bearing.
			continue
		}

		// Ceil, so the interval never exceeds the nominal. Rounding to nearest
		// would let a 1.9× segment become one 1.9× span, which is the case the
		// caller was trying to avoid by asking for a nominal at all.
		divisions := int(math.Ceil(segment / nominalSpanM))
		for step := 1; step < divisions; step++ {
			out = append(out, from.Interpolate(to, float64(step)/float64(divisions)))
		}

		out = append(out, to)
	}

	if err := out.Validate(); err != nil {
		return nil, fmt.Errorf("shape: resampled path is not a line: %w", err)
	}

	return out, nil
}
