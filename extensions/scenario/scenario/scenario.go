// Package scenario models a reproducible simulation: a network model,
// a weather forecast, a hazard configuration, a contingency policy,
// and a random seed, bound together so that one run can be reproduced
// from its record alone.
package scenario

import (
	"fmt"
	"time"

	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Policy is the contingency policy to apply during the scenario.
//
// This is the seam a risk model plugs into. extensions/contingency/engine.go
// documents that contingency ordering "is where a risk model plugs in, and that
// substitution is the coupling the whole project exists to demonstrate". Policy
// is that seam: it determines which contingencies to analyze or how to rank them.
// A two-value enum looks like over-engineering until a reader knows what the
// third value is for — the substitution hook is what the third value enables.
type Policy string

const (
	// PolicyAll screens every in-service branch.
	PolicyAll Policy = "all"

	// PolicyExplicit screens only the branches named in Manifest.Branches.
	//
	// Empty Manifest.Branches with PolicyExplicit is invalid: explicit means
	// nothing would silently become "screen everything", the opposite of what
	// was asked.
	PolicyExplicit Policy = "explicit"
)

// Manifest pins a scenario's components: everything needed to run one
// simulation and reproduce the result.
type Manifest struct {
	// ModelHash names the network model. A scenario cannot be reproduced
	// without it; absent means unknown, and unknown scenarios are not
	// reproducible.
	ModelHash string

	// WeatherCycle identifies a forecast. A cycle spans a whole forecast
	// horizon rather than naming a moment, so naming the cycle alone does
	// not determine which values to use.
	WeatherCycle string

	// ValidAt is the moment within the forecast cycle to analyze: when the
	// thing being forecast is forecast to occur. Separate from the cycle
	// because a cycle is a horizon, not a moment. The cycle supplies the
	// knowledge time (when the forecast was made); this field supplies the
	// valid time. The pair is what makes a manifest determine a result,
	// protecting the reproducibility invariant the type exists to defend.
	ValidAt time.Time

	// HazardConfig identifies the hazard configuration to use: which wind
	// model, thermal model, ice model, etc. Absent means use defaults.
	HazardConfig string

	// Policy determines which contingencies to analyze. See Policy's own
	// comment for the seam it represents.
	Policy Policy

	// Branches lists the contingencies to analyze when Policy is PolicyExplicit.
	// Ignored when Policy is PolicyAll. Must be non-empty when Policy is
	// PolicyExplicit.
	Branches []string

	// Seed is the random seed for reproducibility.
	Seed int64
}

// Validate reports whether a manifest is coherent enough to use.
func (m Manifest) Validate() error {
	if m.ModelHash == "" {
		return fmt.Errorf(
			"%w: a scenario cannot be reproduced without a model hash",
			bserr.ErrInvalid)
	}

	if m.WeatherCycle == "" {
		return fmt.Errorf(
			"%w: a scenario cannot be reproduced without a weather cycle",
			bserr.ErrInvalid)
	}

	if m.ValidAt.IsZero() {
		return fmt.Errorf(
			"%w: a scenario cannot be reproduced without a valid time; "+
				"the cycle is a horizon, not a moment",
			bserr.ErrInvalid)
	}

	if m.Policy == PolicyExplicit && len(m.Branches) == 0 {
		return fmt.Errorf(
			"%w: explicit policy needs a branch list; empty would silently "+
				"become 'screen everything', the opposite of what was asked",
			bserr.ErrInvalid)
	}

	return nil
}

// Scenario is a stored simulation binding: a scenario identifier, name,
// manifest, and temporal metadata.
type Scenario struct {
	// ID identifies this scenario.
	ID id.ID `json:"id"`

	// Name is a human-friendly label.
	Name string `json:"name"`

	// Manifest is the reproducible binding: model, forecast, hazard config,
	// policy, and seed.
	Manifest Manifest `json:"manifest"`

	// CreatedAt is when the scenario was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the scenario was last modified.
	UpdatedAt time.Time `json:"updated_at"`
}
