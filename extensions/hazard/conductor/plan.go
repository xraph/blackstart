package conductor

import "github.com/xraph/blackstart/pkg/id"

// Candidate is one carrier a bulk assumption pass is considering.
//
// The voltage comes from the carrier itself — a circuit states one from the
// source it was imported from — and not from the caller. That is the whole
// difference between assuming a conductor and assuming a network: the guess
// this package makes is about which conductor a line of that class is strung
// with, never about what class it is.
type Candidate struct {
	CarrierID id.ID

	// VoltageKV is what the carrier's own source recorded. Zero means the
	// source did not say, which is a skip rather than a default.
	//
	// The caller must already have reduced this through canonical.StatedNominalV
	// (or the equivalent conversion for a non-bus source) before filling the
	// struct: zero is the only value PlanAssumptions reads as "unstated", so an
	// unreduced value — a raw circuit field with no validation behind it, say —
	// that happens to be negative or infinite would read as stated and produce
	// a wrong plan.
	VoltageKV float64

	// FarEndVoltageKV is the other end's voltage, for a carrier that has two
	// ends. Zero for a circuit, which has one voltage and no far end.
	//
	// Carried so that PlanAssumptions can tell a line from a transformer. A
	// branch whose two ends sit at different voltages is a transformer, and no
	// conductor is strung on one — but the caller that reads the two buses
	// must not be the one that decides, or the single rule stopping this
	// command inventing conductors for transformers would be the only rule not
	// covered by the pure function.
	//
	// Same precondition as VoltageKV: already reduced through
	// canonical.StatedNominalV, zero and only zero meaning "unstated". This is
	// the one definition of "states a voltage" this project keeps —
	// PlanAssumptions's `> 0` transformer check is a second spelling of it, not
	// an independent rule, and it is only safe because every filler of this
	// struct reduces through the same function first.
	FarEndVoltageKV float64

	// Assigned marks a carrier that already has an assignment.
	Assigned bool
}

// Assumption is one carrier and the conductor a pass would give it.
//
// No identifier and no basis. Minting an assignment identifier is a side
// effect, and a plan that produced one could not be computed twice and
// compared — which is what a dry run is for.
type Assumption struct {
	CarrierID     id.ID
	ConductorCode string
}

// Plan is what a bulk assumption pass would do, before it does any of it.
//
// Four buckets rather than a list and a count, because the reasons are not
// interchangeable: a carrier with no voltage is a gap in the source data, one
// already assigned is a decision somebody made, and a transformer is a carrier
// no conductor will ever be strung on. Reporting them as a single "skipped"
// number would leave a reader unable to tell which of those the map is short
// of — and would send somebody hunting for voltage data that would change
// nothing.
//
// Every candidate lands in exactly one bucket. A carrier silently dropped is
// how somebody comes to believe the map is fuller than it is.
type Plan struct {
	Assume          []Assumption
	NoVoltage       []id.ID
	AlreadyAssigned []id.ID
	Transformers    []id.ID
}

// PlanAssumptions decides what each carrier should get, and writes nothing.
//
// Pure so that a dry run and the run that follows it are the same computation,
// rather than two code paths that agree until one of them is edited.
func PlanAssumptions(candidates []Candidate) Plan {
	plan := Plan{}

	for _, candidate := range candidates {
		// An existing assignment wins, whatever its basis. A surveyed row
		// outranks an assumption, and a re-run that quietly demoted one would
		// destroy the only record of somebody having looked.
		if candidate.Assigned {
			plan.AlreadyAssigned = append(plan.AlreadyAssigned, candidate.CarrierID)
			continue
		}

		// A transformer, and therefore not a conductor run. Checked after
		// Assigned because a surveyed row outranks every inference, and before
		// AssumeByVoltage because a transformer that also states nothing at one
		// end is still a transformer.
		//
		// Both ends must state a voltage. One end silent is not evidence of a
		// transformer, and letting absence imply one would contradict the rule
		// the rest of this path keeps: absent means unknown, never a
		// conclusion.
		if candidate.VoltageKV > 0 &&
			candidate.FarEndVoltageKV > 0 &&
			candidate.VoltageKV != candidate.FarEndVoltageKV {
			plan.Transformers = append(plan.Transformers, candidate.CarrierID)

			continue
		}

		// AssumeByVoltage rejects anything that is not a voltage class. Its
		// error is not raised here: one carrier whose source was quiet must not
		// abandon a pass over every other carrier, and the count below is a
		// more useful answer than a failure would be.
		code, err := AssumeByVoltage(candidate.VoltageKV)
		if err != nil {
			plan.NoVoltage = append(plan.NoVoltage, candidate.CarrierID)
			continue
		}

		plan.Assume = append(plan.Assume, Assumption{
			CarrierID:     candidate.CarrierID,
			ConductorCode: code,
		})
	}

	return plan
}
