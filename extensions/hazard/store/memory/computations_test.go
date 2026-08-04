package memory_test

import (
	"errors"
	"testing"
	"time"

	hid "github.com/xraph/blackstart/extensions/hazard/id"
	"github.com/xraph/blackstart/extensions/hazard/store"
	"github.com/xraph/blackstart/extensions/hazard/store/memory"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

func computation(cycle id.ID, at time.Time) store.Computation {
	return store.Computation{
		ID:         hid.NewComputation(),
		CycleID:    cycle,
		RunID:      id.MustNew("prvrun"),
		ComputedAt: at,
		Spans:      12,
		Unrated:    3,
		Values:     60,
	}
}

func TestComputationRoundTrips(t *testing.T) {
	t.Parallel()

	s := memory.New()
	want := computation(id.MustNew("wxcyc"), issuance())

	if err := s.Computations().Create(t.Context(), want); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Computations().Get(t.Context(), want.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got != want {
		t.Errorf("read back %+v, wrote %+v", got, want)
	}
}

func TestCreatingTheSameComputationTwiceConflicts(t *testing.T) {
	t.Parallel()

	s := memory.New()
	want := computation(id.MustNew("wxcyc"), issuance())

	if err := s.Computations().Create(t.Context(), want); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := s.Computations().Create(t.Context(), want); !errors.Is(err, bserr.ErrConflict) {
		t.Fatalf("second Create: %v, want bserr.ErrConflict", err)
	}
}

// Recomputing a cycle after an assignment is corrected produces a second pass,
// and both are kept: a rating already consumed downstream still has the
// computation that made it.
func TestACycleCanHaveSeveralComputations(t *testing.T) {
	t.Parallel()

	s := memory.New()
	cycle := id.MustNew("wxcyc")

	first := computation(cycle, issuance())
	second := computation(cycle, issuance().Add(time.Hour))
	elsewhere := computation(id.MustNew("wxcyc"), issuance())

	for _, in := range []store.Computation{first, second, elsewhere} {
		if err := s.Computations().Create(t.Context(), in); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	got, err := s.Computations().ForCycle(t.Context(), cycle)
	if err != nil {
		t.Fatalf("ForCycle: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("ForCycle gave %d passes, want 2", len(got))
	}

	if got[0].ID != second.ID {
		t.Errorf("ForCycle led with %s, want the newest pass %s", got[0].ID, second.ID)
	}
}

func TestComputationListIsNewestFirst(t *testing.T) {
	t.Parallel()

	s := memory.New()

	for hour := range 4 {
		in := computation(id.MustNew("wxcyc"), issuance().Add(time.Duration(hour)*time.Hour))
		if err := s.Computations().Create(t.Context(), in); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	got, err := s.Computations().List(t.Context(), pkgstore.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(got) != 4 {
		t.Fatalf("listed %d, want 4", len(got))
	}

	for index := 1; index < len(got); index++ {
		if got[index].ComputedAt.After(got[index-1].ComputedAt) {
			t.Errorf("row %d is newer than the one before it", index)
		}
	}
}

func TestInvalidComputationIsRefused(t *testing.T) {
	t.Parallel()

	s := memory.New()

	for name, mutate := range map[string]func(*store.Computation){
		"no cycle":      func(c *store.Computation) { c.CycleID = id.Nil },
		"no time":       func(c *store.Computation) { c.ComputedAt = time.Time{} },
		"wrong prefix":  func(c *store.Computation) { c.ID = id.MustNew("span") },
		"negative span": func(c *store.Computation) { c.Spans = -1 },
	} {
		broken := computation(id.MustNew("wxcyc"), issuance())
		mutate(&broken)

		if err := s.Computations().Create(t.Context(), broken); err == nil {
			t.Errorf("%s: stored", name)
		}
	}
}
