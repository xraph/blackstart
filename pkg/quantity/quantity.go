package quantity

import (
	"fmt"
)

// kelvinOffset is the difference between the Celsius and Kelvin scales.
const kelvinOffset = 273.15

// Quantity is a magnitude with a unit, and — when that unit is per-unit — the
// reference base that gives it absolute meaning.
//
// The fields are unexported, so a Quantity can only be built through a
// constructor that checks its invariants, and can only be read through methods
// that enforce them. In particular there is no way to obtain the SI value of a
// per-unit quantity without supplying a base, because that path does not
// exist rather than because callers are asked not to take it.
//
// The zero Quantity is a dimensionless zero and is safe to use.
type Quantity struct {
	mag  float64
	unit Unit
	base *Base
}

// New builds a quantity in an absolute unit.
//
// It rejects per-unit units: those carry no absolute meaning without a base
// and must go through NewPerUnit.
func New(magnitude float64, unit Unit) (Quantity, error) {
	if unit.PerUnit {
		return Quantity{}, fmt.Errorf(
			"quantity: %s is a per-unit unit and needs a base; use NewPerUnit", unit)
	}

	return Quantity{mag: magnitude, unit: unit}, nil
}

// MustNew is New for compile-time-constant arguments, such as test fixtures
// and named physical constants. It panics rather than returning an error.
func MustNew(magnitude float64, unit Unit) Quantity {
	q, err := New(magnitude, unit)
	if err != nil {
		panic(err)
	}

	return q
}

// NewPerUnit builds a normalized quantity against a reference base.
//
// The base must be non-nil and must define a scale for the unit's dimension.
// Checking that here rather than at conversion time means a Quantity that
// exists is a Quantity that can be converted.
func NewPerUnit(magnitude float64, unit Unit, base *Base) (Quantity, error) {
	if !unit.PerUnit {
		return Quantity{}, fmt.Errorf(
			"quantity: %s is an absolute unit; use New", unit)
	}

	if base == nil {
		return Quantity{}, fmt.Errorf(
			"quantity: a per-unit %s needs a reference base", unit.Dim)
	}

	if _, err := base.scaleFor(unit.Dim); err != nil {
		return Quantity{}, err
	}

	return Quantity{mag: magnitude, unit: unit, base: base}, nil
}

// Megawatts and the constructors below cover the units that appear most often.
// Each builds a Quantity on a unit that cannot fail validation, so unlike New
// they need no error return.
func Megawatts(v float64) Quantity { return Quantity{mag: v, unit: Megawatt} }
func Megavars(v float64) Quantity  { return Quantity{mag: v, unit: Megavar} }
func MegavoltAmps(v float64) Quantity {
	return Quantity{mag: v, unit: MegavoltAmpere}
}
func Kilovolts(v float64) Quantity { return Quantity{mag: v, unit: Kilovolt} }
func Volts(v float64) Quantity     { return Quantity{mag: v, unit: Volt} }
func Amperes(v float64) Quantity   { return Quantity{mag: v, unit: Ampere} }
func Ohms(v float64) Quantity      { return Quantity{mag: v, unit: Ohm} }
func Hertzes(v float64) Quantity   { return Quantity{mag: v, unit: Hertz} }
func Meters(v float64) Quantity    { return Quantity{mag: v, unit: Meter} }
func DegreesC(v float64) Quantity  { return Quantity{mag: v, unit: Celsius} }
func Degrees(v float64) Quantity   { return Quantity{mag: v, unit: Degree} }
func Ratios(v float64) Quantity    { return Quantity{mag: v, unit: Ratio} }

// Magnitude returns the numeric value in this quantity's own unit.
//
// It is deliberately not named Value: a magnitude without its unit is exactly
// the bare float64 this package exists to discourage, and the name should not
// suggest it is the whole story.
func (q Quantity) Magnitude() float64 { return q.mag }

// Unit returns the unit.
func (q Quantity) Unit() Unit { return q.unit }

// Base returns the per-unit reference, or nil for an absolute quantity.
func (q Quantity) Base() *Base { return q.base }

// IsPerUnit reports whether this is a normalized value.
func (q Quantity) IsPerUnit() bool { return q.unit.PerUnit }

// IsZero reports whether the magnitude is exactly zero.
func (q Quantity) IsZero() bool { return q.mag == 0 }

// SI returns the magnitude converted to the SI base unit of its dimension:
// volts, amperes, watts, ohms, siemens, kelvin, meters, seconds, radians.
//
// This is the method that makes the package worth having. A per-unit quantity
// with no base cannot reach this code — NewPerUnit refuses to build one — so
// the error path here covers only dimensions with no defined base scale.
func (q Quantity) SI() (float64, error) {
	if q.unit.PerUnit {
		scale, err := q.base.scaleFor(q.unit.Dim)
		if err != nil {
			return 0, err
		}

		return q.mag * scale, nil
	}

	// Celsius is affine, not multiplicative. A ToSI factor cannot express the
	// offset, so it is applied explicitly here.
	if q.unit == Celsius {
		return q.mag + kelvinOffset, nil
	}

	return q.mag * q.unit.ToSI, nil
}

// MustSI is SI for cases where the quantity is known to be convertible, such
// as persisting a value whose unit is fixed by the schema. It panics rather
// than returning an error.
func (q Quantity) MustSI() float64 {
	v, err := q.SI()
	if err != nil {
		panic(err)
	}

	return v
}
