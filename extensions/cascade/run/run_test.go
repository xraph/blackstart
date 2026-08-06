package run_test

import (
	"errors"
	"testing"
	"time"

	cid "github.com/xraph/blackstart/extensions/cascade/id"
	"github.com/xraph/blackstart/extensions/cascade/run"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

func validRun(t *testing.T) run.CascadeRun {
	t.Helper()

	return run.CascadeRun{
		ID:            cid.NewRun(),
		ModelHash:     "abc123",
		SolverVersion: "0.1.0-test",
		Outcome:       run.OutcomeSettled,
		StartedAt:     time.Now(),
		FinishedAt:    time.Now(),
	}
}

// Absent and empty are different, and Validate is where the third state is
// refused rather than stored.
//
// Deleting the CanonicalHash check in Validate makes this fail: a pointer to ""
// would store, and would then match a listing for the empty hash that no caller
// intended.
func TestAPresentButEmptyCanonicalHashIsRefused(t *testing.T) {
	r := validRun(t)
	blank := ""
	r.CanonicalHash = &blank

	err := r.Validate()
	if err == nil {
		t.Fatal("Validate accepted a canonical hash of no characters")
	}

	if !errors.Is(err, bserr.ErrInvalid) {
		t.Errorf("got %v, want a wrapped bserr.ErrInvalid", err)
	}

	// A run that simply does not know its canonical hash is the ordinary case
	// — every run recorded before the field existed — and must still store.
	r.CanonicalHash = nil

	if err := r.Validate(); err != nil {
		t.Errorf("Validate refused a run with no canonical hash: %v", err)
	}
}

// One network, two names, and either one finds it.
//
// Comparing only ModelHash in NamesModel fails the canonical case; comparing
// only CanonicalHash fails the protobuf case, which every run stored before
// this field depends on.
func TestARunIsNamedByEitherOfItsHashes(t *testing.T) {
	canonical := "canonical-side"

	r := validRun(t)
	r.ModelHash = "protobuf-side"
	r.CanonicalHash = &canonical

	for _, hash := range []string{"protobuf-side", "canonical-side"} {
		if !r.NamesModel(hash) {
			t.Errorf("a run does not answer to %q, which is one of its own hashes", hash)
		}
	}

	if r.NamesModel("some-other-network") {
		t.Error("a run answers to a hash that is neither of its own")
	}

	// And a run that never learned its canonical hash answers to no empty
	// one, or every such run in the store lists together as one network.
	r.CanonicalHash = nil

	if r.NamesModel("") {
		t.Error("a run with no canonical hash answers to the empty hash")
	}
}

// A run that cannot name what it simulated, or what produced it, cannot be
// reproduced — Validate is the store's only gate against recording one.
func TestARunNeedsAnIdentifierAModelAndAVersion(t *testing.T) {
	cases := map[string]func(run.CascadeRun) run.CascadeRun{
		"no identifier": func(r run.CascadeRun) run.CascadeRun {
			r.ID = id.Nil

			return r
		},
		"no model hash": func(r run.CascadeRun) run.CascadeRun {
			r.ModelHash = ""

			return r
		},
		"no solver version": func(r run.CascadeRun) run.CascadeRun {
			r.SolverVersion = ""

			return r
		},
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := mutate(validRun(t))

			err := r.Validate()
			if err == nil {
				t.Fatalf("Validate accepted a run with %s", name)
			}

			if !errors.Is(err, bserr.ErrInvalid) {
				t.Errorf("got %v, want a wrapped bserr.ErrInvalid", err)
			}
		})
	}
}

func TestAWellFormedRunValidates(t *testing.T) {
	if err := validRun(t).Validate(); err != nil {
		t.Errorf("Validate rejected a well-formed run: %v", err)
	}
}
