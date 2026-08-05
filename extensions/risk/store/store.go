package store

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/store"
)

// Quantity names a series this extension writes.
//
// Two, and they are different kinds of thing rather than two ways of saying
// one. A conductor fails between two structures at a rate proportional to how
// much of it is exposed, so its probability scales with span length. A
// structure fails at a point according to what it is built like, so its
// probability carries no length at all. Storing them as one column would force
// a choice about which structure a shared one belongs to, and there is no
// right answer to that.
type Quantity string

const (
	// QuantityConductorFailure is P(the conductor on this span fails) in the
	// hour ending at ValidAt.
	QuantityConductorFailure Quantity = "conductor_failure"

	// QuantityStructureFailure is P(this structure fails) in the same hour.
	QuantityStructureFailure Quantity = "structure_failure"
)

// Quantities returns every quantity this extension writes.
func Quantities() []Quantity {
	return []Quantity{QuantityConductorFailure, QuantityStructureFailure}
}

// Valid reports whether the quantity is one this extension knows.
func (q Quantity) Valid() bool { return slices.Contains(Quantities(), q) }

// ComponentValue is one probability, for one component, at one moment.
//
// ComponentID is a span or a structure — the prefix says which. This
// extension mints no component identifiers of its own, because it computes
// about things that already exist.
//
// Scalar rides along with every row rather than being applied and discarded.
// Until the calibration piece fits one, it is 1.0; once it does, a
// probability computed under an implicit 1.0 and one computed under a fitted
// 0.7 are not the same claim, and only the stored scalar can tell a backtest
// which is which.
type ComponentValue struct {
	ComponentID id.ID     `json:"component_id"`
	Quantity    Quantity  `json:"quantity"`
	IssuedAt    time.Time `json:"issued_at"`
	ValidAt     time.Time `json:"valid_at"`
	Probability float64   `json:"probability"`
	Scalar      float64   `json:"scalar"`
}

// Validate reports whether a value is storable.
func (v ComponentValue) Validate() error {
	if v.ComponentID.IsZero() {
		return fmt.Errorf("%w: value names no component", bserr.ErrInvalid)
	}

	if !v.Quantity.Valid() {
		return fmt.Errorf(
			"%w: quantity %q is not one of %v", bserr.ErrInvalid, v.Quantity, Quantities())
	}

	if v.IssuedAt.IsZero() || v.ValidAt.IsZero() {
		return fmt.Errorf(
			"%w: %s value for %s has issuance %v and valid time %v; both axes are "+
				"required", bserr.ErrInvalid, v.Quantity, v.ComponentID, v.IssuedAt, v.ValidAt)
	}

	return nil
}

// Store is everything the risk extension persists.
type Store interface {
	store.Lifecycle

	// Values reads and writes the computed probabilities.
	Values() ValueStore
}

// ValueStore holds the numbers.
type ValueStore interface {
	// PutBatch writes a pass's values.
	PutBatch(ctx context.Context, in []ComponentValue) error

	// ForComponent returns one component's series over a window, ascending by
	// ValidAt, for the given issuance.
	ForComponent(ctx context.Context, component id.ID, quantities []Quantity,
		issuedAt, from, to time.Time) ([]ComponentValue, error)
}
