package hazard_test

import (
	"errors"
	"testing"

	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/hazard/conductor"
	hid "github.com/xraph/blackstart/extensions/hazard/id"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// A bulk pass assigns a conductor to every carrier that states a voltage.
func TestAssumeWritesAnAssignmentPerVoltedCarrier(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	engine := w.engine(t)
	carrier := id.MustNew(gid.Circuit)

	plan, err := engine.Assume(t.Context(), []conductor.Candidate{
		{CarrierID: carrier, VoltageKV: 345},
	}, false)
	if err != nil {
		t.Fatalf("Assume: %v", err)
	}

	if len(plan.Assume) != 1 {
		t.Fatalf("planned %d assumptions, want 1", len(plan.Assume))
	}

	got, err := engine.AssignmentForCarrier(t.Context(), carrier)
	if err != nil {
		t.Fatalf("AssignmentForCarrier: %v", err)
	}

	// The basis is the point. A row written by a bulk pass that recorded
	// itself as surveyed would be indistinguishable from one somebody checked.
	if got.Basis != conductor.AssumedByVoltage {
		t.Errorf("basis = %q, want %q", got.Basis, conductor.AssumedByVoltage)
	}
}

// A surveyed assignment survives a bulk pass.
//
// The one that matters. A re-run that quietly demoted a surveyed row to an
// assumption would destroy the only record of somebody having looked, and
// nothing downstream would report anything different afterwards.
func TestAssumeLeavesASurveyedAssignmentAlone(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	engine := w.engine(t)
	carrier := id.MustNew(gid.Circuit)

	surveyed := conductor.Assignment{
		ID:            hid.NewAssignment(),
		CarrierID:     carrier,
		ConductorCode: "hawk",
		Basis:         conductor.Surveyed,
		Note:          "work order 4471",
	}
	if err := engine.Assign(t.Context(), surveyed); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	// 345 kV would assume cardinal, so a pass that overwrote would be visible
	// in the conductor as well as in the basis.
	if _, err := engine.Assume(t.Context(), []conductor.Candidate{
		{CarrierID: carrier, VoltageKV: 345, Assigned: true},
	}, false); err != nil {
		t.Fatalf("Assume: %v", err)
	}

	got, err := engine.AssignmentForCarrier(t.Context(), carrier)
	if err != nil {
		t.Fatalf("AssignmentForCarrier: %v", err)
	}

	if got.Basis != conductor.Surveyed || got.ConductorCode != "hawk" {
		t.Errorf("assignment became %s/%s, want surveyed/hawk", got.Basis, got.ConductorCode)
	}

	if got.Note != "work order 4471" {
		t.Errorf("note = %q, want the surveyor's own", got.Note)
	}
}

// A dry run reports without writing.
//
// This is not a convenience. It is the instrument that measures how much of a
// network can be widened at all, and a measurement that changed what it
// measured would be worth nothing.
func TestAssumeDryRunWritesNothing(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	engine := w.engine(t)
	carrier := id.MustNew(gid.Circuit)

	plan, err := engine.Assume(t.Context(), []conductor.Candidate{
		{CarrierID: carrier, VoltageKV: 345},
	}, true)
	if err != nil {
		t.Fatalf("Assume: %v", err)
	}

	if len(plan.Assume) != 1 {
		t.Errorf("a dry run planned %d assumptions, want 1", len(plan.Assume))
	}

	if _, err := engine.AssignmentForCarrier(t.Context(), carrier); !errorsIsNotFound(err) {
		t.Errorf("a dry run wrote an assignment: %v", err)
	}
}

// A carrier whose source stated no voltage is counted, not defaulted.
func TestAssumeCountsACarrierWithNoVoltage(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	engine := w.engine(t)
	quiet := id.MustNew(gid.Circuit)

	plan, err := engine.Assume(
		t.Context(), []conductor.Candidate{{CarrierID: quiet}}, false)
	if err != nil {
		t.Fatalf("Assume: %v", err)
	}

	if len(plan.NoVoltage) != 1 {
		t.Errorf("NoVoltage = %v, want the one quiet carrier", plan.NoVoltage)
	}

	if _, err := engine.AssignmentForCarrier(t.Context(), quiet); !errorsIsNotFound(err) {
		t.Errorf("a carrier with no voltage was assigned anyway: %v", err)
	}
}

// Every candidate is accounted for, so the counts a command prints add up to
// what it was handed. A carrier silently dropped is how somebody comes to
// believe the map is fuller than it is.
func TestAssumeAccountsForEveryCarrier(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	engine := w.engine(t)
	assigned := id.MustNew(gid.Circuit)

	if err := engine.Assign(t.Context(), conductor.Assignment{
		ID:            hid.NewAssignment(),
		CarrierID:     assigned,
		ConductorCode: "drake",
		Basis:         conductor.Surveyed,
	}); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	plan, err := engine.Assume(t.Context(), []conductor.Candidate{
		{CarrierID: id.MustNew(gid.Circuit), VoltageKV: 345},
		{CarrierID: id.MustNew(gid.Circuit)},
		{CarrierID: assigned, VoltageKV: 230, Assigned: true},
	}, false)
	if err != nil {
		t.Fatalf("Assume: %v", err)
	}

	total := len(plan.Assume) + len(plan.NoVoltage) + len(plan.AlreadyAssigned)
	if total != 3 {
		t.Errorf("accounted for %d of 3 carriers", total)
	}
}

func errorsIsNotFound(err error) bool {
	return err != nil && errors.Is(err, bserr.ErrNotFound)
}
