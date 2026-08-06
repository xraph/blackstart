package memory_test

import (
	"context"
	"errors"
	"testing"

	nmid "github.com/xraph/blackstart/extensions/networkmodel/id"
	"github.com/xraph/blackstart/extensions/networkmodel/model"
	"github.com/xraph/blackstart/extensions/networkmodel/store/memory"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

func bus(modelID id.ID, ordinal string, volts float64) model.Bus {
	return model.Bus{
		ID:        nmid.NewBus(),
		ModelID:   modelID,
		Ordinal:   ordinal,
		VNominalV: volts,
	}
}

func TestBusesRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	modelID := nmid.NewModel()

	in := []model.Bus{bus(modelID, "bus-1", 345000), bus(modelID, "bus-2", 138000)}

	if err := store.Buses().PutAll(ctx, modelID, in); err != nil {
		t.Fatalf("writing buses: %v", err)
	}

	got, err := store.Buses().ForModel(ctx, modelID)
	if err != nil {
		t.Fatalf("reading buses: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d buses, want 2", len(got))
	}

	if got[0].VNominalV != 345000 {
		t.Fatalf("bus-1 came back at %v volts, want 345000", got[0].VNominalV)
	}
}

// Numeric rather than lexical, for the reason branches sort numerically:
// bus-10 between bus-1 and bus-2 is not an ordering anybody reading a network
// expects.
func TestBusesComeBackInNumericOrdinalOrder(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	modelID := nmid.NewModel()

	in := []model.Bus{bus(modelID, "bus-10", 0), bus(modelID, "bus-2", 0)}

	if err := store.Buses().PutAll(ctx, modelID, in); err != nil {
		t.Fatalf("writing buses: %v", err)
	}

	got, err := store.Buses().ForModel(ctx, modelID)
	if err != nil {
		t.Fatalf("reading buses: %v", err)
	}

	if got[0].Ordinal != "bus-2" || got[1].Ordinal != "bus-10" {
		t.Fatalf("got %s then %s, want bus-2 then bus-10", got[0].Ordinal, got[1].Ordinal)
	}
}

func TestByOrdinalFindsOneBus(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	modelID := nmid.NewModel()

	if err := store.Buses().PutAll(ctx, modelID, []model.Bus{
		bus(modelID, "bus-1", 345000),
		bus(modelID, "bus-2", 138000),
	}); err != nil {
		t.Fatalf("writing buses: %v", err)
	}

	got, err := store.Buses().ByOrdinal(ctx, modelID, "bus-2")
	if err != nil {
		t.Fatalf("reading bus-2: %v", err)
	}

	if got.VNominalV != 138000 {
		t.Fatalf("bus-2 states %v volts, want 138000", got.VNominalV)
	}
}

// Not-found rather than a zero value. A caller handed a zero-volt bus cannot
// tell "this bus states nothing" from "this bus is not here", and those want
// different reporting.
func TestByOrdinalReportsAMissingBus(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	modelID := nmid.NewModel()

	_, err := store.Buses().ByOrdinal(ctx, modelID, "bus-9")
	if !errors.Is(err, bserr.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

// A re-import writes the same ordinals again. Merging would leave two rows
// answering to one name, which every lookup afterwards resolves arbitrarily.
func TestPutAllReplacesRatherThanMerges(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	modelID := nmid.NewModel()

	if err := store.Buses().PutAll(ctx, modelID, []model.Bus{bus(modelID, "bus-1", 345000)}); err != nil {
		t.Fatalf("first write: %v", err)
	}

	if err := store.Buses().PutAll(ctx, modelID, []model.Bus{bus(modelID, "bus-1", 138000)}); err != nil {
		t.Fatalf("second write: %v", err)
	}

	got, err := store.Buses().ForModel(ctx, modelID)
	if err != nil {
		t.Fatalf("reading buses: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("got %d buses after a rewrite, want 1", len(got))
	}

	if got[0].VNominalV != 138000 {
		t.Fatalf("bus-1 states %v volts, want the rewritten 138000", got[0].VNominalV)
	}
}

// AddMissing on an empty model inserts everything it is handed, matching a
// fresh write.
func TestAddMissingOnAnEmptyModelInsertsEveryBus(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	modelID := nmid.NewModel()

	in := []model.Bus{bus(modelID, "bus-1", 345000), bus(modelID, "bus-2", 138000)}

	n, err := store.Buses().AddMissing(ctx, modelID, in)
	if err != nil {
		t.Fatalf("AddMissing: %v", err)
	}

	if n != len(in) {
		t.Errorf("AddMissing inserted %d, want %d", n, len(in))
	}
}

// The partial case: bus-1 is already stored, bus-2 is missing. AddMissing
// must insert only bus-2 and leave bus-1's row — identifier included —
// exactly as it was, discarding the freshly-minted bus-1 a repair would also
// hand it.
func TestAddMissingInsertsOnlyTheMissingBusAndKeepsTheRest(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	modelID := nmid.NewModel()

	existing := bus(modelID, "bus-1", 345000)
	if err := store.Buses().PutAll(ctx, modelID, []model.Bus{existing}); err != nil {
		t.Fatalf("PutAll: %v", err)
	}

	freshOne := bus(modelID, "bus-1", 345000)
	two := bus(modelID, "bus-2", 138000)

	n, err := store.Buses().AddMissing(ctx, modelID, []model.Bus{freshOne, two})
	if err != nil {
		t.Fatalf("AddMissing: %v", err)
	}

	if n != 1 {
		t.Fatalf("AddMissing inserted %d, want 1 (only bus-2)", n)
	}

	got, err := store.Buses().ByOrdinal(ctx, modelID, "bus-1")
	if err != nil {
		t.Fatalf("ByOrdinal(bus-1): %v", err)
	}

	if got.ID != existing.ID {
		t.Errorf("bus-1's identifier changed: got %s, want the original %s", got.ID, existing.ID)
	}

	if _, err := store.Buses().ByOrdinal(ctx, modelID, "bus-2"); err != nil {
		t.Errorf("ByOrdinal(bus-2) after AddMissing: %v", err)
	}
}

// The fully-complete case: every ordinal already exists, so AddMissing
// inserts nothing and no existing identifier changes.
func TestAddMissingOnACompleteModelInsertsNoBuses(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	modelID := nmid.NewModel()

	in := []model.Bus{bus(modelID, "bus-1", 345000), bus(modelID, "bus-2", 138000)}
	if err := store.Buses().PutAll(ctx, modelID, in); err != nil {
		t.Fatalf("PutAll: %v", err)
	}

	fresh := []model.Bus{bus(modelID, "bus-1", 345000), bus(modelID, "bus-2", 138000)}

	n, err := store.Buses().AddMissing(ctx, modelID, fresh)
	if err != nil {
		t.Fatalf("AddMissing: %v", err)
	}

	if n != 0 {
		t.Errorf("AddMissing inserted %d on a complete model, want 0", n)
	}

	got, err := store.Buses().ForModel(ctx, modelID)
	if err != nil {
		t.Fatalf("ForModel: %v", err)
	}

	for i, b := range got {
		if b.ID != in[i].ID {
			t.Errorf("bus %s's identifier changed: got %s, want the original %s", b.Ordinal, b.ID, in[i].ID)
		}
	}
}

// A bus claiming a different model than the one it is written under is a
// programming error that would otherwise store and confuse a later ForModel.
func TestABusMustBelongToTheModelItIsWrittenUnder(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	modelID := nmid.NewModel()

	err := store.Buses().PutAll(ctx, modelID, []model.Bus{bus(nmid.NewModel(), "bus-1", 0)})
	if !errors.Is(err, bserr.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
}
