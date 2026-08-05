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
	VoltageKV float64

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
// Three buckets rather than a list and a count, because the reasons are not
// interchangeable: a carrier with no voltage is a gap in the source data, and
// one already assigned is a decision somebody made. Reporting them as a single
// "skipped" number would leave a reader unable to tell which of those two the
// map is short of.
//
// Every candidate lands in exactly one bucket. A carrier silently dropped is
// how somebody comes to believe the map is fuller than it is.
type Plan struct {
	Assume          []Assumption
	NoVoltage       []id.ID
	AlreadyAssigned []id.ID
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
