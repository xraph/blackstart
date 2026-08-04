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

func validRun(t *testing.T) run.Run {
	t.Helper()

	return run.Run{
		ID:            cid.NewRun(),
		ModelHash:     "abc123",
		SolverVersion: "0.1.0-test",
		Outcome:       run.OutcomeSettled,
		StartedAt:     time.Now(),
		FinishedAt:    time.Now(),
	}
}

// A run that cannot name what it simulated, or what produced it, cannot be
// reproduced — Validate is the store's only gate against recording one.
func TestARunNeedsAnIdentifierAModelAndAVersion(t *testing.T) {
	cases := map[string]func(run.Run) run.Run{
		"no identifier": func(r run.Run) run.Run {
			r.ID = id.Nil

			return r
		},
		"no model hash": func(r run.Run) run.Run {
			r.ModelHash = ""

			return r
		},
		"no solver version": func(r run.Run) run.Run {
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
