package quantity

import (
	"fmt"
	"math"
)

// In converts to another unit of the same physical quantity.
//
// The target must be Compatible: same dimension, same kind, and same
// per-unit-ness. Converting between per-unit and absolute is a separate
// operation — see ToAbsolute and ToPerUnit — because it needs a base and is
// worth being explicit about.
func (q Quantity) In(target Unit) (Quantity, error) {
	if !q.unit.Compatible(target) {
		return Quantity{}, incompatible("convert", q.unit, target)
	}

	if q.unit == target {
		return q, nil
	}

	if target.PerUnit {
		// Same dimension, same kind, both per-unit: the same base still
		// applies, and per-unit has no sub-scales, so nothing changes.
		return Quantity{mag: q.mag, unit: target, base: q.base}, nil
	}

	si, err := q.SI()
	if err != nil {
		return Quantity{}, err
	}

	if target == Celsius {
		return Quantity{mag: si - kelvinOffset, unit: target}, nil
	}

	return Quantity{mag: si / target.ToSI, unit: target}, nil
}

// ToAbsolute converts a per-unit quantity to an absolute unit using its base.
//
// It is an error to call this on a quantity that is already absolute, so that
// a double conversion is caught rather than silently applying the base twice.
func (q Quantity) ToAbsolute(target Unit) (Quantity, error) {
	if !q.unit.PerUnit {
		return Quantity{}, fmt.Errorf(
			"quantity: %s is already absolute", q.unit)
	}

	if target.PerUnit {
		return Quantity{}, fmt.Errorf(
			"quantity: ToAbsolute needs an absolute target, got %s", target)
	}

	if q.unit.Dim != target.Dim || q.unit.Kind != target.Kind {
		return Quantity{}, incompatible("convert", q.unit, target)
	}

	si, err := q.SI()
	if err != nil {
		return Quantity{}, err
	}

	return Quantity{mag: si / target.ToSI, unit: target}, nil
}

// ToPerUnit normalizes an absolute quantity against a base.
func (q Quantity) ToPerUnit(target Unit, base *Base) (Quantity, error) {
	if q.unit.PerUnit {
		return Quantity{}, fmt.Errorf("quantity: %s is already per-unit", q.unit)
	}

	if !target.PerUnit {
		return Quantity{}, fmt.Errorf(
			"quantity: ToPerUnit needs a per-unit target, got %s", target)
	}

	if q.unit.Dim != target.Dim || q.unit.Kind != target.Kind {
		return Quantity{}, incompatible("convert", q.unit, target)
	}

	if base == nil {
		return Quantity{}, fmt.Errorf(
			"quantity: normalizing a %s needs a reference base", q.unit.Dim)
	}

	scale, err := base.scaleFor(target.Dim)
	if err != nil {
		return Quantity{}, err
	}

	si, err := q.SI()
	if err != nil {
		return Quantity{}, err
	}

	return Quantity{mag: si / scale, unit: target, base: base}, nil
}

// IsFinite reports whether the magnitude is a real number. Solver output and
// parsed input are both worth checking before they enter the model.
func (q Quantity) IsFinite() bool {
	return !math.IsInf(q.mag, 0) && !math.IsNaN(q.mag)
}

// String renders the quantity for logs and error messages.
func (q Quantity) String() string {
	if q.unit.PerUnit {
		return fmt.Sprintf("%.6g pu [%s, %s]", q.mag, q.unit.Dim, q.base)
	}

	if q.unit.Symbol == "" {
		return fmt.Sprintf("%.6g", q.mag)
	}

	return fmt.Sprintf("%.6g %s", q.mag, q.unit.Symbol)
}

func incompatible(op string, a, b Unit) error {
	return fmt.Errorf("quantity: cannot %s %s to %s: %s",
		op, a, b, explainMismatch(a, b))
}

// explainMismatch says which of the three compatibility conditions failed.
// The distinction matters when reading a stack trace: a dimension mismatch is
// usually a modelling error, while a per-unit mismatch is usually a missing
// conversion.
func explainMismatch(a, b Unit) string {
	switch {
	case a.Dim != b.Dim:
		return fmt.Sprintf("different dimensions (%s vs %s)", a.Dim, b.Dim)
	case a.Kind != b.Kind:
		return fmt.Sprintf("different kinds (%s vs %s)", a.Kind, b.Kind)
	case a.PerUnit != b.PerUnit:
		return "one is per-unit and the other is absolute"
	default:
		return "incompatible units"
	}
}
