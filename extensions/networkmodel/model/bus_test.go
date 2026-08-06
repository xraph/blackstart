package model_test

import (
	"errors"
	"testing"

	nmid "github.com/xraph/blackstart/extensions/networkmodel/id"
	"github.com/xraph/blackstart/extensions/networkmodel/model"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

func validBus() model.Bus {
	return model.Bus{
		ID:        nmid.NewBus(),
		ModelID:   nmid.NewModel(),
		Ordinal:   "bus-1",
		VNominalV: 138000,
	}
}

func TestAValidBusValidates(t *testing.T) {
	if err := validBus().Validate(); err != nil {
		t.Fatalf("a valid bus was refused: %v", err)
	}
}

// A bus that states no voltage is the ordinary case for the IEEE cases, which
// carry no base kV. It must store, because the alternative is an import that
// fails on a legitimate file.
func TestABusThatStatesNoVoltageIsValid(t *testing.T) {
	bus := validBus()
	bus.VNominalV = 0

	if err := bus.Validate(); err != nil {
		t.Fatalf("a bus stating no voltage was refused: %v", err)
	}
}

// Negative is not "unknown" — it is a number nothing could have measured, and
// storing it would let a consumer that forgot StatedNominalV convert against it.
func TestANegativeVoltageIsRefused(t *testing.T) {
	bus := validBus()
	bus.VNominalV = -1

	if err := bus.Validate(); !errors.Is(err, bserr.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
}

func TestABusNeedsAnIdentifier(t *testing.T) {
	bus := validBus()
	bus.ID = id.ID{}

	if err := bus.Validate(); !errors.Is(err, bserr.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
}

// The prefix check catches a branch identifier handed to a bus row, which
// would otherwise store and only fail at a lookup that returns nothing.
func TestABusIdentifierMustCarryTheBusPrefix(t *testing.T) {
	bus := validBus()
	bus.ID = nmid.NewBranch()

	if err := bus.Validate(); !errors.Is(err, bserr.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
}

func TestABusNeedsAModel(t *testing.T) {
	bus := validBus()
	bus.ModelID = id.ID{}

	if err := bus.Validate(); !errors.Is(err, bserr.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
}

// The ordinal is what Branch.FromBus and Placement.Bus hold. A blank one is a
// row nothing can join to.
func TestABusNeedsAnOrdinal(t *testing.T) {
	bus := validBus()
	bus.Ordinal = "  "

	if err := bus.Validate(); !errors.Is(err, bserr.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
}
