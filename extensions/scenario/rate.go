package scenario

import (
	"context"
	"errors"
	"math"
	"time"

	solverv1 "github.com/xraph/blackstart/proto/gen/go/blackstart/solver/v1"
)

// Rated is what the weather did to a model's branch ratings.
//
// The counts are plain ints, unlike the *int pair on scenario.Run that mirrors
// them. By the time a Rated exists the rating step has run, so absence is not
// representable and a zero is a real measurement. On a Run it is the opposite:
// a queued run has not rated anything yet, and nil is how it says so.
type Rated struct {
	// Overrides carries one rating override per weather-rated branch, in the
	// model's own branch order.
	//
	// Ordered rather than merely collected: the solver applies overrides in
	// sequence and a scenario has to produce the same request twice, so the
	// list cannot depend on map iteration or on which lookup finished first.
	Overrides []*solverv1.Override

	// WeatherRated is how many branches the weather actually reached.
	//
	// Reported rather than inferred from len(Overrides) by the caller, because
	// this number is the one a reader needs alongside BranchCount: three
	// branches rated out of two thousand and two thousand rated out of two
	// thousand produce reports that otherwise look identical, and the second
	// is the one a reader assumes.
	WeatherRated int

	// BranchCount is every branch in the model, rated or not.
	BranchCount int
}

// rate derives dynamic line ratings for a model from the weather.
//
// The chain, per branch, is: the route carrying it, that route's spans, each
// span's ampacity at the pinned forecast times, the weakest of those, and the
// conversion of that current into the volt-amperes the solver contract states.
//
// A branch whose chain breaks anywhere keeps its static rating and emits no
// override. That is not leniency: a rating of zero would make the branch
// infinitely overloaded and trip the whole network on the first screen, and an
// incomplete asset record for one line is a gap in the geometry rather than a
// reason to abandon the scenario. §4.2 of the design is what makes the
// *aggregate* case loud — a cycle that rated nothing is the run's failure to
// raise, not this function's, because only the caller knows which cycle was
// pinned.
func rate(
	ctx context.Context,
	model *solverv1.NetworkModel,
	geo GeoReader,
	hazard HazardReader,
	issuedAt, validAt time.Time,
) (Rated, error) {
	if model == nil {
		return Rated{}, errors.New("scenario: rating needs a network model")
	}

	if geo == nil || hazard == nil {
		return Rated{}, errors.New(
			"scenario: rating needs both a geometry and a hazard reader; the " +
				"chain runs through geo and cannot be completed from either alone")
	}

	branches := model.GetBranches()

	// The contract carries one line-to-line voltage base for the whole model,
	// so it is also the only voltage a branch has here. Non-positive means the
	// model is wrong rather than merely unrated, and every conversion below
	// would silently produce zero or a negative rating.
	volts := model.GetVBaseV()
	if !(volts > 0) || math.IsInf(volts, 0) {
		return Rated{}, errors.New(
			"scenario: the model states no usable voltage base; a current " +
				"cannot be converted to volt-amperes without one")
	}

	out := Rated{
		Overrides:   make([]*solverv1.Override, 0, len(branches)),
		BranchCount: len(branches),
	}

	// Index order, because the override's branch field is an index into this
	// same list and because two runs of the same scenario have to produce
	// byte-identical requests.
	//
	// Out-of-service branches included. The contract keeps them in the model
	// so a contingency can restore one without shifting every index, and a
	// branch restored mid-screen needs the rating the weather gives it as much
	// as any other. Skipping them would leave exactly those on nameplate.
	for index, branch := range branches {
		amperes, ok := weakestSpan(ctx, geo, hazard, branch.GetId(), issuedAt, validAt)
		if !ok {
			continue
		}

		out.Overrides = append(out.Overrides, &solverv1.Override{
			Change: &solverv1.Override_BranchRating{
				BranchRating: &solverv1.BranchRating{
					Branch:   uint32(index),
					RatingVa: voltAmperes(volts, amperes),
				},
			},
		})
		out.WeatherRated++
	}

	return out, nil
}
