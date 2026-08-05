package conductor_test

import (
	"testing"

	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/hazard/conductor"
	"github.com/xraph/blackstart/pkg/id"
)

func circuit() id.ID { return id.MustNew(gid.Circuit) }

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
	})

	total := len(got.Assume) + len(got.NoVoltage) + len(got.AlreadyAssigned)
	if total != 4 {
		t.Errorf("accounted for %d of 4 candidates", total)
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
