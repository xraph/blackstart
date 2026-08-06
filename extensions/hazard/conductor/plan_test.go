package conductor_test

import (
	"testing"

	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/hazard/conductor"
	nmid "github.com/xraph/blackstart/extensions/networkmodel/id"
	"github.com/xraph/blackstart/pkg/id"
)

func circuit() id.ID { return id.MustNew(gid.Circuit) }

func branch() id.ID { return id.MustNew(nmid.Branch) }

// A carrier that states a voltage gets a conductor typical of its class.
func TestPlanAssumesFromTheCarriersOwnVoltage(t *testing.T) {
	t.Parallel()

	high, low := circuit(), circuit()

	got := conductor.PlanAssumptions([]conductor.Candidate{
		{CarrierID: high, VoltageKV: 345},
		{CarrierID: low, VoltageKV: 138},
	})

	if len(got.Assume) != 2 {
		t.Fatalf("planned %d assumptions, want 2", len(got.Assume))
	}

	// Different classes must produce different conductors, or the voltage was
	// read and then ignored — which is the failure that looks like success.
	if got.Assume[0].ConductorCode == got.Assume[1].ConductorCode {
		t.Errorf("345 kV and 138 kV both assumed %q", got.Assume[0].ConductorCode)
	}
}

// A carrier whose source never stated a voltage is skipped and counted.
//
// Not defaulted. An assignment recording assumed_by_voltage where the voltage
// was not the element's spends the basis field — the entire mechanism by which
// a consumer knows how far to trust a rating — to raise a coverage number.
func TestPlanSkipsACarrierWithNoVoltage(t *testing.T) {
	t.Parallel()

	quiet := circuit()

	got := conductor.PlanAssumptions([]conductor.Candidate{{CarrierID: quiet}})

	if len(got.Assume) != 0 {
		t.Errorf("planned %d assumptions for a carrier with no voltage", len(got.Assume))
	}

	if len(got.NoVoltage) != 1 || got.NoVoltage[0] != quiet {
		t.Errorf("NoVoltage = %v, want the one quiet carrier", got.NoVoltage)
	}
}

// A negative voltage is not a voltage class either, and must not reach
// AssumeByVoltage as an error that abandons the whole run.
func TestPlanTreatsAnImpossibleVoltageAsAbsent(t *testing.T) {
	t.Parallel()

	got := conductor.PlanAssumptions([]conductor.Candidate{
		{CarrierID: circuit(), VoltageKV: -1},
	})

	if len(got.Assume) != 0 || len(got.NoVoltage) != 1 {
		t.Errorf("planned %d assumptions and %d skips, want 0 and 1",
			len(got.Assume), len(got.NoVoltage))
	}
}

// An existing assignment is left alone.
//
// A surveyed assignment outranks an assumption, and a re-run that quietly
// demoted one would destroy the only record of somebody having looked.
func TestPlanLeavesAnAssignedCarrierAlone(t *testing.T) {
	t.Parallel()

	known := circuit()

	got := conductor.PlanAssumptions([]conductor.Candidate{
		{CarrierID: known, VoltageKV: 345, Assigned: true},
	})

	if len(got.Assume) != 0 {
		t.Errorf("planned %d assumptions over an existing assignment", len(got.Assume))
	}

	if len(got.AlreadyAssigned) != 1 {
		t.Errorf("AlreadyAssigned = %v, want the one known carrier", got.AlreadyAssigned)
	}
}

// Every candidate lands in exactly one bucket, so the counts a command prints
// add up to what it was given. A carrier silently dropped is how somebody comes
// to believe the map is fuller than it is.
func TestPlanAccountsForEveryCandidate(t *testing.T) {
	t.Parallel()

	got := conductor.PlanAssumptions([]conductor.Candidate{
		{CarrierID: circuit(), VoltageKV: 345},
		{CarrierID: circuit()},
		{CarrierID: circuit(), VoltageKV: 230, Assigned: true},
		{CarrierID: circuit(), VoltageKV: 115},
		{CarrierID: branch(), VoltageKV: 345, FarEndVoltageKV: 138},
	})

	total := len(got.Assume) + len(got.NoVoltage) + len(got.AlreadyAssigned) + len(got.Transformers)
	if total != 5 {
		t.Errorf("accounted for %d of 5 candidates", total)
	}
}

// A transformer is not a conductor run. Assigning it a conductor would put an
// assumed_by_voltage row against an element no conductor is ever strung on.
func TestABranchWhoseEndsDifferIsATransformer(t *testing.T) {
	t.Parallel()

	carrier := branch()

	plan := conductor.PlanAssumptions([]conductor.Candidate{{
		CarrierID:       carrier,
		VoltageKV:       345,
		FarEndVoltageKV: 138,
	}})

	if len(plan.Transformers) != 1 {
		t.Fatalf("got %d transformers, want 1", len(plan.Transformers))
	}

	if len(plan.Assume) != 0 {
		t.Fatalf("a transformer was assigned %v", plan.Assume)
	}
}

// One end stating nothing is not evidence of a transformer. The rest of this
// path reads absence as unknown and never lets it imply a conclusion.
func TestOneStatedEndIsNotATransformer(t *testing.T) {
	t.Parallel()

	carrier := branch()

	plan := conductor.PlanAssumptions([]conductor.Candidate{{
		CarrierID:       carrier,
		VoltageKV:       138,
		FarEndVoltageKV: 0,
	}})

	if len(plan.Transformers) != 0 {
		t.Fatalf("a half-stated branch was called a transformer")
	}

	if len(plan.Assume) != 1 {
		t.Fatalf("got %d assumptions, want 1", len(plan.Assume))
	}
}

// Two ends at one voltage is a line, which is the ordinary case.
func TestMatchingEndsAreAssumedNormally(t *testing.T) {
	t.Parallel()

	carrier := branch()

	plan := conductor.PlanAssumptions([]conductor.Candidate{{
		CarrierID:       carrier,
		VoltageKV:       138,
		FarEndVoltageKV: 138,
	}})

	if len(plan.Assume) != 1 || plan.Assume[0].ConductorCode != "hawk" {
		t.Fatalf("got %v, want one hawk", plan.Assume)
	}
}

// Already-assigned wins over every other bucket. A surveyed row outranks an
// assumption whatever else is true of the carrier, and a re-run that demoted
// one would destroy the only record of somebody having looked.
func TestAnAssignedTransformerIsReportedAsAssigned(t *testing.T) {
	t.Parallel()

	carrier := branch()

	plan := conductor.PlanAssumptions([]conductor.Candidate{{
		CarrierID:       carrier,
		VoltageKV:       345,
		FarEndVoltageKV: 138,
		Assigned:        true,
	}})

	if len(plan.AlreadyAssigned) != 1 {
		t.Fatalf("got %d already-assigned, want 1", len(plan.AlreadyAssigned))
	}

	if len(plan.Transformers) != 0 {
		t.Fatalf("an assigned carrier was also bucketed as a transformer")
	}
}

// Transformer precedes no-voltage: it is the more specific statement, and
// reporting a transformer as a data gap invites somebody to go and fill a gap
// that will never produce an assignment.
func TestATransformerStatingNothingAtOneEndIsStillNoVoltage(t *testing.T) {
	t.Parallel()

	carrier := branch()

	plan := conductor.PlanAssumptions([]conductor.Candidate{{
		CarrierID:       carrier,
		VoltageKV:       0,
		FarEndVoltageKV: 138,
	}})

	if len(plan.NoVoltage) != 1 {
		t.Fatalf("got %d no-voltage, want 1", len(plan.NoVoltage))
	}
}

// The planned conductor has to exist in the library, or the assignment it
// becomes fails validation three steps later with nothing pointing back here.
func TestPlanNamesAConductorTheLibraryHas(t *testing.T) {
	t.Parallel()

	got := conductor.PlanAssumptions([]conductor.Candidate{
		{CarrierID: circuit(), VoltageKV: 500},
	})

	if len(got.Assume) != 1 {
		t.Fatalf("planned %d assumptions, want 1", len(got.Assume))
	}

	if _, err := conductor.ByCode(got.Assume[0].ConductorCode); err != nil {
		t.Errorf("planned conductor %q: %v", got.Assume[0].ConductorCode, err)
	}
}
