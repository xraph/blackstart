package provenance_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/provenance"
	pvid "github.com/xraph/blackstart/extensions/provenance/id"
	"github.com/xraph/blackstart/extensions/provenance/record"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/hooks"
)

// A run naming a producer that was never recorded is a broken chain, and a
// broken chain is worse than a missing one: it looks traceable.
func TestARunCannotNameAProducerThatDoesNotExist(t *testing.T) {
	e := engine(t, nil)

	_, err := e.RecordRun(t.Context(),
		record.Run{ModelVersionID: pvid.NewModel(), Kind: "powerflow"}, nil)

	if err == nil {
		t.Fatal("RecordRun accepted a run naming a producer that was never recorded")
	}

	if !errors.Is(err, bserr.ErrInvalid) {
		t.Errorf("got %v, want it to wrap ErrInvalid", err)
	}
}

// Likewise for a dangling input edge — and the error has to name the run, so
// the half-written chain can be found.
func TestADanglingInputNamesTheRunItBroke(t *testing.T) {
	e := engine(t, nil)

	solver, err := e.RecordModel(t.Context(), producer())
	if err != nil {
		t.Fatalf("RecordModel: %v", err)
	}

	_, err = e.RecordRun(t.Context(),
		record.Run{ModelVersionID: solver.ID, Kind: "powerflow"},
		[]provenance.Input{{DatasetVersionID: pvid.NewDataset(), Role: record.RoleInput}},
	)

	if err == nil {
		t.Fatal("RecordRun accepted a lineage edge to a dataset that does not exist")
	}

	if !strings.Contains(err.Error(), "chain is incomplete") {
		t.Errorf("the error does not say the chain broke: %v", err)
	}
}

// A run over a synthetic case reads no dataset and is still reproducible. What
// would make it irreproducible is not knowing which code ran.
func TestARunWithNoInputsIsStillReproducible(t *testing.T) {
	e := engine(t, nil)

	solver, err := e.RecordModel(t.Context(), producer())
	if err != nil {
		t.Fatalf("RecordModel: %v", err)
	}

	run, err := e.RecordRun(t.Context(),
		record.Run{ModelVersionID: solver.ID, Kind: "powerflow"}, nil)
	if err != nil {
		t.Fatalf("RecordRun: %v", err)
	}

	trace, err := e.Trace(t.Context(), run.ID)
	if err != nil {
		t.Fatalf("Trace: %v", err)
	}

	if !trace.Reproducible() {
		t.Error("a run over a synthetic case was reported as irreproducible")
	}
}

func TestRecordingEmitsDomainEvents(t *testing.T) {
	bus := hooks.NewBus()

	var seen []string
	bus.SubscribeAll(func(_ context.Context, e hooks.Event) error {
		seen = append(seen, e.Name)

		return nil
	})

	e := engine(t, bus)

	if _, err := e.RecordDataset(t.Context(), dataset(hashA)); err != nil {
		t.Fatalf("RecordDataset: %v", err)
	}

	if len(seen) != 1 || seen[0] != "provenance.dataset.recorded" {
		t.Errorf("events %v", seen)
	}
}

// A nil bus is the normal case for a CLI invocation.
func TestANilBusIsFine(t *testing.T) {
	if _, err := engine(t, nil).RecordDataset(t.Context(), dataset(hashA)); err != nil {
		t.Fatalf("RecordDataset with no bus: %v", err)
	}
}
