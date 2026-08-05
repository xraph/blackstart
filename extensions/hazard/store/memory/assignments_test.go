package memory_test

import (
	"errors"
	"testing"

	"github.com/xraph/blackstart/extensions/hazard/conductor"
	"github.com/xraph/blackstart/extensions/hazard/store/memory"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

func TestAssignmentRoundTrips(t *testing.T) {
	t.Parallel()

	s := memory.New()
	branch := id.MustNew("brch")
	want := assignment(branch)

	if err := s.Assignments().Put(t.Context(), want); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Assignments().Get(t.Context(), want.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got != want {
		t.Errorf("read back %+v, wrote %+v", got, want)
	}

	byBranch, err := s.Assignments().ForCarrier(t.Context(), branch)
	if err != nil {
		t.Fatalf("ForBranch: %v", err)
	}

	if byBranch.ID != want.ID {
		t.Errorf("ForBranch gave %s, wrote %s", byBranch.ID, want.ID)
	}
}

// A branch has one conductor. Two assignments would leave nothing to say which
// one a rating was computed against, so the second replaces the first — which
// is also the normal case, a survey correcting an assumption.
func TestASecondAssignmentReplacesTheFirst(t *testing.T) {
	t.Parallel()

	s := memory.New()
	branch := id.MustNew("brch")

	assumed := assignment(branch)
	assumed.ConductorCode = "hawk"
	assumed.Basis = conductor.AssumedByVoltage

	if err := s.Assignments().Put(t.Context(), assumed); err != nil {
		t.Fatalf("Put: %v", err)
	}

	surveyed := assignment(branch)

	if err := s.Assignments().Put(t.Context(), surveyed); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Assignments().ForCarrier(t.Context(), branch)
	if err != nil {
		t.Fatalf("ForBranch: %v", err)
	}

	if got.ID != surveyed.ID || got.ConductorCode != "drake" {
		t.Errorf("ForBranch gave %+v, want the surveyed one", got)
	}

	if _, err := s.Assignments().Get(t.Context(), assumed.ID); !errors.Is(err, bserr.ErrNotFound) {
		t.Errorf("the replaced assignment is still readable: %v", err)
	}

	all, err := s.Assignments().List(t.Context(), pkgstore.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(all) != 1 {
		t.Errorf("listed %d assignments for one branch", len(all))
	}
}

// An unassigned branch is not an error the engine reports — it is a span
// counted unrated. It has to be distinguishable from a lookup failure, which
// is why this asserts the sentinel rather than merely that err is non-nil.
func TestAnUnassignedBranchIsNotFound(t *testing.T) {
	t.Parallel()

	s := memory.New()

	_, err := s.Assignments().ForCarrier(t.Context(), id.MustNew("brch"))
	if !errors.Is(err, bserr.ErrNotFound) {
		t.Fatalf("ForBranch error = %v, want bserr.ErrNotFound", err)
	}
}

func TestInvalidAssignmentIsRefused(t *testing.T) {
	t.Parallel()

	s := memory.New()
	broken := assignment(id.MustNew("brch"))
	broken.ConductorCode = "nosuchconductor"

	if err := s.Assignments().Put(t.Context(), broken); err == nil {
		t.Fatal("an assignment naming a conductor outside the library was stored")
	}
}

func TestDeletingAnAssignmentLeavesTheBranchUnrated(t *testing.T) {
	t.Parallel()

	s := memory.New()
	branch := id.MustNew("brch")
	want := assignment(branch)

	if err := s.Assignments().Put(t.Context(), want); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := s.Assignments().Delete(t.Context(), want.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := s.Assignments().ForCarrier(t.Context(), branch); !errors.Is(err, bserr.ErrNotFound) {
		t.Errorf("ForBranch after Delete: %v, want bserr.ErrNotFound", err)
	}

	if err := s.Assignments().Delete(t.Context(), want.ID); !errors.Is(err, bserr.ErrNotFound) {
		t.Errorf("deleting twice: %v, want bserr.ErrNotFound", err)
	}
}

func TestAssignmentListPagesForward(t *testing.T) {
	t.Parallel()

	s := memory.New()

	for range 5 {
		if err := s.Assignments().Put(t.Context(), assignment(id.MustNew("brch"))); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	first, err := s.Assignments().List(t.Context(), pkgstore.Page{Limit: 2})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(first) != 2 {
		t.Fatalf("first page has %d rows, want 2", len(first))
	}

	next, err := s.Assignments().List(
		t.Context(), pkgstore.Page{Limit: 2, After: first[1].ID})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(next) != 2 {
		t.Fatalf("second page has %d rows, want 2", len(next))
	}

	for _, got := range next {
		if got.ID.String() <= first[1].ID.String() {
			t.Errorf("the cursor returned %s, which is not after %s",
				got.ID, first[1].ID)
		}
	}
}
