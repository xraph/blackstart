package quantity

import (
	"fmt"
	"math"
)

// Sqrt3 is the three-phase line-to-line factor. Go's math package offers
// Sqrt2, SqrtE, SqrtPi and SqrtPhi, but not this one.
//
// Exported because it is not only per-unit machinery: anything converting
// between a line current and a three-phase apparent power needs the same
// factor, and a second spelling of it elsewhere in the tree is how the two
// drift apart. bs-quantity states the identical relationship on the Rust side.
const Sqrt3 = 1.7320508075688772935

// Base is the reference against which per-unit values are normalized.
//
// Power-systems models are almost always expressed in per-unit: impedances,
// voltages, flows and limits are all fractions of a reference. That reference
// is a property of the network model and the voltage level, not of any
// individual value, which is why a Quantity holds a pointer to a shared Base
// rather than copying the numbers.
//
// Conventions, which are the standard three-phase ones and are worth stating
// because getting them wrong is a silent factor-of-root-three error:
//
//   - SBase is the three-phase apparent power base, in volt-amperes.
//   - VBase is the line-to-line voltage base, in volts.
//   - ZBase = VBase^2 / SBase
//   - IBase = SBase / (sqrt(3) * VBase)
//
// A system typically has one SBase (100 MVA is conventional) and a different
// VBase per voltage level, so a network model carries a Base per voltage
// level, all sharing an SBase.
type Base struct {
	// sBase is three-phase apparent power in volt-amperes.
	sBase float64

	// vBase is line-to-line voltage in volts.
	vBase float64
}

// NewBase constructs a per-unit base from the units engineers actually quote:
// three-phase MVA and line-to-line kV.
//
// Both must be positive and finite. A zero or negative base is not a degenerate
// case to be tolerated — it means the network model is wrong, and every value
// normalized against it would be meaningless.
func NewBase(sBaseMVA, vBaseKV float64) (*Base, error) {
	if !isPositiveFinite(sBaseMVA) {
		return nil, fmt.Errorf(
			"quantity: apparent power base must be positive and finite, got %v MVA", sBaseMVA)
	}

	if !isPositiveFinite(vBaseKV) {
		return nil, fmt.Errorf(
			"quantity: voltage base must be positive and finite, got %v kV", vBaseKV)
	}

	return &Base{sBase: sBaseMVA * 1e6, vBase: vBaseKV * 1e3}, nil
}

// MustNewBase is NewBase for cases where the arguments are compile-time
// constants, such as test fixtures and the conventional 100 MVA system base.
// It panics rather than returning an error.
func MustNewBase(sBaseMVA, vBaseKV float64) *Base {
	b, err := NewBase(sBaseMVA, vBaseKV)
	if err != nil {
		panic(err)
	}

	return b
}

// SBase returns the three-phase apparent power base in volt-amperes.
func (b *Base) SBase() float64 { return b.sBase }

// VBase returns the line-to-line voltage base in volts.
func (b *Base) VBase() float64 { return b.vBase }

// ZBase returns the impedance base in ohms: VBase^2 / SBase.
func (b *Base) ZBase() float64 { return b.vBase * b.vBase / b.sBase }

// YBase returns the admittance base in siemens: the reciprocal of ZBase.
func (b *Base) YBase() float64 { return b.sBase / (b.vBase * b.vBase) }

// IBase returns the line current base in amperes: SBase / (sqrt(3) * VBase).
//
// The sqrt(3) is the three-phase line-to-line convention. Omitting it is a
// classic error that produces currents wrong by 73%, which is large enough to
// look like a modelling mistake rather than a units mistake.
func (b *Base) IBase() float64 { return b.sBase / (Sqrt3 * b.vBase) }

// scaleFor returns the SI value of one per-unit of the given dimension.
//
// This is the single place that maps a dimension onto its base, and therefore
// the single place a per-unit value acquires absolute meaning.
func (b *Base) scaleFor(dim Dimension) (float64, error) {
	switch dim {
	case Voltage:
		return b.VBase(), nil
	case Current:
		return b.IBase(), nil
	case Power:
		return b.SBase(), nil
	case Impedance:
		return b.ZBase(), nil
	case Admittance:
		return b.YBase(), nil
	case Dimensionless:
		return 1, nil
	default:
		return 0, fmt.Errorf(
			"quantity: no per-unit base defined for dimension %s", dim)
	}
}

// Equal reports whether two bases are the same reference. Nil bases are equal
// only to each other.
func (b *Base) Equal(other *Base) bool {
	if b == nil || other == nil {
		return b == other
	}

	return b.sBase == other.sBase && b.vBase == other.vBase
}

// String renders the base in the units it was declared in.
func (b *Base) String() string {
	if b == nil {
		return "<no base>"
	}

	return fmt.Sprintf("%.4g MVA / %.4g kV", b.sBase/1e6, b.vBase/1e3)
}

func isPositiveFinite(v float64) bool {
	return v > 0 && !math.IsInf(v, 0) && !math.IsNaN(v)
}
