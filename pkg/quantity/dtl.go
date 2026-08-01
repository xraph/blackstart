package quantity

import (
	"encoding/json"
	"fmt"
)

// This file projects a Quantity into the two map-shaped forms it travels in:
// the Formula engine's, and JSON. They share a shape so that a quantity reads
// identically in an API response and in a formula.
//
// DTL has no custom-type or operator-overload mechanism — its value domain is
// bare Go `any` over nil/bool/int64/float64/string/time.Time/[]any/
// map[string]any — so a Quantity crosses that boundary as a plain map.
//
// The three key names below are a PUBLIC CONTRACT. They appear in every
// user-authored formula as v.magnitude, v.unit and v.base. Renaming one does
// not break a build; it breaks saved work. Do not change them.
//
// The magnitude is float64 for a related reason: DTL's toFloat returns 0 for
// any type it does not recognize, without an error, so a decimal or big.Float
// would arrive in a formula as a silent zero.
const (
	DTLKeyMagnitude = "magnitude"
	DTLKeyUnit      = "unit"
	DTLKeyBase      = "base"
)

// ToDTL projects the quantity into the map form the Formula engine consumes.
func (q Quantity) ToDTL() map[string]any {
	return map[string]any{
		DTLKeyMagnitude: q.mag,
		DTLKeyUnit:      UnitName(q.unit),
		DTLKeyBase:      FormatBase(q.base),
	}
}

// FromDTL reconstructs a quantity from the Formula engine's map form.
func FromDTL(m map[string]any) (Quantity, error) {
	rawMag, ok := m[DTLKeyMagnitude]
	if !ok {
		return Quantity{}, fmt.Errorf("quantity: missing %q", DTLKeyMagnitude)
	}

	mag, err := toFloat64(rawMag)
	if err != nil {
		return Quantity{}, err
	}

	rawUnit, _ := m[DTLKeyUnit].(string)

	unit, err := ParseUnit(rawUnit)
	if err != nil {
		return Quantity{}, err
	}

	rawBase, _ := m[DTLKeyBase].(string)

	base, err := ParseBase(rawBase)
	if err != nil {
		return Quantity{}, err
	}

	if unit.PerUnit {
		return NewPerUnit(mag, unit, base)
	}

	return New(mag, unit)
}

// toFloat64 accepts the numeric shapes an expression language may produce.
// It refuses anything else rather than coercing to zero, which is what DTL's
// own toFloat does and is the behavior this wrapper exists to avoid.
func toFloat64(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case int64:
		return float64(n), nil
	case int:
		return float64(n), nil
	default:
		return 0, fmt.Errorf(
			"quantity: magnitude must be numeric, got %T", v)
	}
}

// jsonQuantity is the wire form. It mirrors the DTL keys so a quantity reads
// identically in an API response and in a formula.
type jsonQuantity struct {
	Magnitude float64 `json:"magnitude"`
	Unit      string  `json:"unit"`
	Base      string  `json:"base,omitempty"`
}

// MarshalJSON implements json.Marshaler.
func (q Quantity) MarshalJSON() ([]byte, error) {
	return json.Marshal(jsonQuantity{
		Magnitude: q.mag,
		Unit:      UnitName(q.unit),
		Base:      FormatBase(q.base),
	})
}

// UnmarshalJSON implements json.Unmarshaler.
func (q *Quantity) UnmarshalJSON(data []byte) error {
	var wire jsonQuantity
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}

	unit, err := ParseUnit(wire.Unit)
	if err != nil {
		return err
	}

	base, err := ParseBase(wire.Base)
	if err != nil {
		return err
	}

	var built Quantity

	if unit.PerUnit {
		built, err = NewPerUnit(wire.Magnitude, unit, base)
	} else {
		built, err = New(wire.Magnitude, unit)
	}

	if err != nil {
		return err
	}

	*q = built

	return nil
}
