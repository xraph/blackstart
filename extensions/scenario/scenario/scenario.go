// Package scenario models a reproducible simulation: a network model,
// a weather forecast, a hazard configuration, a contingency policy,
// and a random seed, bound together so that one run can be reproduced
// from its record alone.
package scenario

import (
	"fmt"
	"slices"
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
	ModelHash string `json:"model_hash"`

	// WeatherCycle identifies a forecast. A cycle spans a whole forecast
	// horizon rather than naming a moment, so naming the cycle alone does
	// not determine which values to use.
	WeatherCycle string `json:"weather_cycle"`

	// ValidAt is the moment within the forecast cycle to analyze: when the
	// thing being forecast is forecast to occur. Separate from the cycle
	// because a cycle is a horizon, not a moment. The cycle supplies the
	// knowledge time (when the forecast was made); this field supplies the
	// valid time. The pair is what makes a manifest determine a result,
	// protecting the reproducibility invariant the type exists to defend.
	ValidAt time.Time `json:"valid_at"`

	// HazardConfig names the hazard configuration a run should use: which
	// wind model, thermal model, ice model.
	//
	// RECORDED, NOT YET APPLIED. Nothing in the pipeline reads it. The rating
	// step consumes ampacities hazard has ALREADY computed and stored, so a
	// run takes whatever configuration that computation pass used and this
	// field changes nothing: two scenarios differing only here produce
	// byte-identical results while each claims a binding it did not use. It
	// is kept rather than dropped because the manifest is the place that
	// pin belongs, and removing it would have to be undone.
	//
	// Honoring it needs a hazard surface that does not exist: computed
	// values are keyed (span, quantity, issuance) with no configuration in
	// the key, so hazard cannot store two configurations of one cycle side by
	// side, and there is nothing for this extension to select between. Until
	// that key grows a configuration — or hazard offers a compute-on-demand
	// call — this field must stay documented as inert. A manifest that
	// over-claims is the failure this whole extension exists to prevent.
	HazardConfig string `json:"hazard_config,omitempty"`

	// Policy determines which contingencies to analyze. See Policy's own
	// comment for the seam it represents.
	Policy Policy `json:"policy"`

	// Branches lists the contingencies to analyze when Policy is PolicyExplicit.
	// Ignored when Policy is PolicyAll. Must be non-empty when Policy is
	// PolicyExplicit.
	Branches []string `json:"branches,omitempty"`

	// Seed is the random seed a run would be reproducible under.
	//
	// RECORDED, NOT YET APPLIED, for the same reason HazardConfig above is and
	// with the same consequence: two scenarios differing only in this field
	// produce byte-identical sweeps and cascades, while each record claims a
	// distinct seed binding. A reader comparing them would conclude the result
	// is seed-insensitive, when the seed was never an input to it.
	//
	// Nothing reads it because there is nothing yet for it to fix. The screen
	// and the cascade are deterministic given a model and a set of overrides —
	// no crate under rust/ draws a random number — so the pipeline's
	// reproducibility rests on the frozen manifest, not on a seed. Honoring
	// this field needs two things that do not exist: something stochastic to
	// seed, and a seed on the wire. proto/blackstart/solver/v1/solver.proto has
	// no such field, and neither contingency.Request nor cascade.Request
	// accepts one, so there is nowhere to put it even if a run wanted to.
	//
	// It is kept rather than dropped because the manifest is where that pin
	// belongs the day a stochastic step arrives — a Monte Carlo outage draw is
	// the obvious one — and removing it would have to be undone. Until then a
	// manifest that over-claims is the failure this whole extension exists to
	// prevent.
	Seed int64 `json:"seed"`
}

// Clone returns a manifest that shares no memory with this one.
//
// The freeze that makes a run citable depends on it. A Manifest is a value,
// so assigning one copies every scalar it holds — but Branches is a slice,
// and a copied struct shares its backing array with the original. Without
// this, editing a scenario's branch list would reach every run already queued
// from it, through the copy, and "same manifest, same result" would be a
// claim nobody could check. The one field that needs it today is the one the
// compiler cannot warn about.
func (m Manifest) Clone() Manifest {
	m.Branches = slices.Clone(m.Branches)

	return m
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

	// The enum itself, before the branch list that depends on it. Everything
	// downstream tests for PolicyExplicit and treats "not that" as PolicyAll,
	// so a typo does not fail — it screens every in-service branch and stores
	// a manifest whose 30-entry branch list describes a run that did not
	// happen. Nothing later can catch it: by then the two readings are
	// indistinguishable.
	//
	// Absent is refused along with a typo, deliberately, because Policy's zero
	// value is "" and defaulting it here would reinstate exactly the silent
	// reading this check exists to stop. A scenario stored before this check
	// still reads, enqueues, executes and finishes — ScenarioRun.Validate does
	// not call this — so the only thing that fails on a legacy row is saving
	// it again, which is the moment someone is looking at it anyway.
	if m.Policy != PolicyAll && m.Policy != PolicyExplicit {
		return fmt.Errorf(
			"%w: policy %q is neither %q nor %q; an unrecognized policy is read as "+
				"%q everywhere downstream, so accepting one would record a branch "+
				"list against a run that screened the whole network",
			bserr.ErrInvalid, m.Policy, PolicyAll, PolicyExplicit, PolicyAll)
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
