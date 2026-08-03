package memory_test

import (
	"errors"
	"testing"

	"github.com/xraph/blackstart/extensions/weather/forecast"
	"github.com/xraph/blackstart/extensions/weather/store"
	"github.com/xraph/blackstart/extensions/weather/store/memory"
	"github.com/xraph/blackstart/pkg/bserr"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

// ForIssuance is the pipeline's idempotency key, so it has to find a cycle by
// its natural key rather than only by identifier.
func TestCycleForIssuance(t *testing.T) {
	t.Parallel()

	s := memory.New()
	want := aCycle()

	if err := s.Cycles().Create(t.Context(), want); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Cycles().ForIssuance(t.Context(), forecast.ModelHRRR, issuance)
	if err != nil {
		t.Fatalf("ForIssuance: %v", err)
	}

	if got.ID != want.ID {
		t.Errorf("ForIssuance returned %s, want %s", got.ID, want.ID)
	}
}

// The same issuance under a different model is a different cycle. GFS and
// HRRR both run at 12Z and they are not the same forecast.
func TestCycleForIssuanceIsPerModel(t *testing.T) {
	t.Parallel()

	s := memory.New()
	hrrr := aCycle()

	if err := s.Cycles().Create(t.Context(), hrrr); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err := s.Cycles().ForIssuance(t.Context(), forecast.ModelGFS, issuance)
	if !errors.Is(err, bserr.ErrNotFound) {
		t.Fatalf("ForIssuance(GFS) error = %v, want ErrNotFound", err)
	}
}

// Two cycles of one model at one issuance is a contradiction rather than a
// duplicate: nothing could say which run the samples came from.
func TestCycleCreateRejectsDuplicateIssuance(t *testing.T) {
	t.Parallel()

	s := memory.New()

	if err := s.Cycles().Create(t.Context(), aCycle()); err != nil {
		t.Fatalf("Create: %v", err)
	}

	err := s.Cycles().Create(t.Context(), aCycle())
	if !errors.Is(err, bserr.ErrConflict) {
		t.Fatalf("second Create error = %v, want ErrConflict", err)
	}
}

func TestCycleSetStatus(t *testing.T) {
	t.Parallel()

	s := memory.New()
	cycle := aCycle()

	if err := s.Cycles().Create(t.Context(), cycle); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := s.Cycles().SetStatus(
		t.Context(), cycle.ID, forecast.StatusSampled,
	); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	got, err := s.Cycles().Get(t.Context(), cycle.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Status != forecast.StatusSampled {
		t.Errorf("status = %q, want %q", got.Status, forecast.StatusSampled)
	}
}

func TestCycleSetStatusRejectsUnknownStatus(t *testing.T) {
	t.Parallel()

	s := memory.New()
	cycle := aCycle()

	if err := s.Cycles().Create(t.Context(), cycle); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := s.Cycles().SetStatus(t.Context(), cycle.ID, "halfway"); err == nil {
		t.Fatal("SetStatus accepted a status outside the set")
	}
}

func TestCycleListFilters(t *testing.T) {
	t.Parallel()

	s := memory.New()

	hrrr := aCycle()
	if err := s.Cycles().Create(t.Context(), hrrr); err != nil {
		t.Fatalf("Create: %v", err)
	}

	gfs := aCycle()
	gfs.Model = forecast.ModelGFS
	gfs.Status = forecast.StatusSampled

	if err := s.Cycles().Create(t.Context(), gfs); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Cycles().List(
		t.Context(), store.CycleFilter{Model: forecast.ModelGFS}, pkgstore.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(got) != 1 || got[0].ID != gfs.ID {
		t.Fatalf("List by model returned %d rows, want just the GFS cycle", len(got))
	}

	got, err = s.Cycles().List(
		t.Context(), store.CycleFilter{Status: forecast.StatusPending}, pkgstore.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(got) != 1 || got[0].ID != hrrr.ID {
		t.Fatalf("List by status returned %d rows, want just the pending cycle", len(got))
	}
}
