package quantity

import (
	"errors"
	"fmt"
	"math"
)

// This file provides aggregation and comparison only: adding load, totalling
// generation, checking a flow against a limit.
//
// It deliberately does NOT provide Mul and Div. Deriving a product's dimension
// is easy, but deriving its Kind is not — I^2 R is active power and I^2 X is
// reactive power, and nothing in the operands distinguishes them. Guessing
// would reintroduce exactly the confusion Kind exists to prevent.
//
// That gap is also the project rule: no power-systems mathematics in Go except
// unit conversion. Products and quotients of electrical quantities are the
// solver's work, in Rust, where the physics is explicit. If a genuine Go use
// case appears, add the operator then, with the Kind question answered by that
// use case rather than in the abstract.

// Add returns the sum, in the receiver's unit.
//
// The operands must be Compatible — same dimension, same kind, same
// per-unit-ness — and, when per-unit, must share a base. Two per-unit voltages
// on different voltage levels are different quantities that happen to be
// written the same way, and summing them is meaningless.
func (q Quantity) Add(other Quantity) (Quantity, error) {
	converted, err := q.align(other, "add")
	if err != nil {
		return Quantity{}, err
	}

	return Quantity{mag: q.mag + converted, unit: q.unit, base: q.base}, nil
}

// Sub returns the difference, in the receiver's unit.
func (q Quantity) Sub(other Quantity) (Quantity, error) {
	converted, err := q.align(other, "subtract")
	if err != nil {
		return Quantity{}, err
	}

	return Quantity{mag: q.mag - converted, unit: q.unit, base: q.base}, nil
}

// Cmp compares two quantities, returning -1, 0 or 1 as the receiver is less
// than, equal to, or greater than the operand.
//
// This is how a flow is checked against a rating. Comparing raw magnitudes
// across units is a real bug — 900 kW is not greater than 1 MW — and this
// method exists so that comparison goes through the same unit checks as
// addition.
func (q Quantity) Cmp(other Quantity) (int, error) {
	converted, err := q.align(other, "compare")
	if err != nil {
		return 0, err
	}

	switch {
	case q.mag < converted:
		return -1, nil
	case q.mag > converted:
		return 1, nil
	default:
		return 0, nil
	}
}

// Equal reports whether two quantities are the same to within a relative
// tolerance. Exact float equality is almost never what is wanted after a
// conversion has happened.
func (q Quantity) Equal(other Quantity, relTol float64) (bool, error) {
	converted, err := q.align(other, "compare")
	if err != nil {
		return false, err
	}

	diff := math.Abs(q.mag - converted)
	scale := math.Max(math.Abs(q.mag), math.Abs(converted))

	if scale == 0 {
		return diff == 0, nil
	}

	return diff/scale <= relTol, nil
}

// Scale multiplies by a dimensionless factor, leaving the unit unchanged.
//
// This covers the cases that would otherwise want Mul — derating a rating,
// applying a loss factor, converting a percentage — without needing to derive
// a new dimension.
func (q Quantity) Scale(factor float64) Quantity {
	return Quantity{mag: q.mag * factor, unit: q.unit, base: q.base}
}

// Neg returns the quantity with its sign reversed. Load convention and
// generation convention differ by exactly this.
func (q Quantity) Neg() Quantity {
	return Quantity{mag: -q.mag, unit: q.unit, base: q.base}
}

// Abs returns the magnitude without its sign, which is what a limit check
// wants for a flow that may run either way along a branch.
func (q Quantity) Abs() Quantity {
	return Quantity{mag: math.Abs(q.mag), unit: q.unit, base: q.base}
}

// Sum totals a slice of quantities, in the unit of the first element.
//
// An empty slice is an error rather than a zero: the caller would have no way
// to know what unit that zero was in, and a dimensionless zero silently
// entering a megawatt calculation is the kind of thing this package exists to
// stop.
func Sum(quantities []Quantity) (Quantity, error) {
	if len(quantities) == 0 {
		return Quantity{}, errors.New(
			"quantity: cannot sum an empty slice; the unit of the result would be unknown")
	}

	total := quantities[0]

	for i, q := range quantities[1:] {
		var err error

		total, err = total.Add(q)
		if err != nil {
			return Quantity{}, fmt.Errorf("quantity: summing element %d: %w", i+1, err)
		}
	}

	return total, nil
}

// align validates that other may be combined with q, and returns other's
// magnitude expressed in q's unit.
func (q Quantity) align(other Quantity, op string) (float64, error) {
	if !q.unit.Compatible(other.unit) {
		return 0, incompatible(op, q.unit, other.unit)
	}

	if q.unit.PerUnit && !q.base.Equal(other.base) {
		return 0, fmt.Errorf(
			"quantity: cannot %s per-unit values on different bases (%s vs %s); "+
				"convert to absolute units first", op, q.base, other.base)
	}

	if q.unit == other.unit {
		return other.mag, nil
	}

	converted, err := other.In(q.unit)
	if err != nil {
		return 0, err
	}

	return converted.mag, nil
}
