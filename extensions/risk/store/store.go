package store

import "slices"

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
