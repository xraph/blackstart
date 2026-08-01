// Package quantity provides typed physical quantities for power-systems work.
//
// Electrical quantities in this codebase are never bare float64. Mixing
// per-unit and SI values, or adding megawatts to megavars, are the two
// characteristic bug classes of power-systems software, and both produce a
// plausible wrong answer rather than a crash.
//
// The package is layered deliberately:
//
//   - Dimension is pure physics — an exponent vector over the SI base
//     dimensions, with multiplication and division defined on it.
//   - Unit adds a symbol, a conversion factor to SI, and a Kind that
//     distinguishes quantities sharing a dimension but not a meaning.
//   - Quantity adds a magnitude and, for per-unit values, a reference Base.
//
// Dimension and Unit are usable with no magnitude in hand. That is a
// requirement, not an accident: the Formula engine's dimensional checker walks
// an expression tree before any value exists, so it reasons entirely in terms
// of units. See a private design note and pkg/quantity/dtl.go.
package quantity

import "strconv"

// Dimension is an exponent vector over the SI base dimensions.
//
// Only the five that arise in this domain are modelled. Amount of substance
// and luminous intensity have no use in power-systems or meteorological work
// and are omitted rather than carried as permanent zeroes.
//
// Dimension is comparable with ==, which is what makes the arithmetic checks
// in Quantity cheap.
type Dimension struct {
	L int8 // length, meter
	M int8 // mass, kilogram
	T int8 // time, second
	I int8 // electric current, ampere
	K int8 // thermodynamic temperature, kelvin
}

// The dimensions this codebase uses. Derived forms are written in terms of the
// base exponents so the arithmetic below is checkable by hand.
//
//nolint:gochecknoglobals // a fixed physical vocabulary, not mutable state
var (
	// Dimensionless covers ratios, per-unit values, power factor, and angles.
	Dimensionless = Dimension{}

	Length      = Dimension{L: 1}
	Mass        = Dimension{M: 1}
	Time        = Dimension{T: 1}
	Current     = Dimension{I: 1}
	Temperature = Dimension{K: 1}

	// Frequency: s^-1.
	Frequency = Dimension{T: -1}

	// Power: kg m^2 s^-3. Shared by active, reactive and apparent power —
	// they are distinguished by Unit.Kind, not by dimension, because they are
	// dimensionally identical and semantically distinct.
	Power = Dimension{L: 2, M: 1, T: -3}

	// Energy: kg m^2 s^-2.
	Energy = Dimension{L: 2, M: 1, T: -2}

	// Voltage: kg m^2 s^-3 A^-1.
	Voltage = Dimension{L: 2, M: 1, T: -3, I: -1}

	// Impedance: kg m^2 s^-3 A^-2.
	Impedance = Dimension{L: 2, M: 1, T: -3, I: -2}

	// Admittance is the reciprocal of impedance.
	Admittance = Dimension{L: -2, M: -1, T: 3, I: 2}
)

// Mul returns the dimension of a product: exponents add.
//
// This is what makes V x I = S check out rather than being asserted:
// Voltage{2,1,-3,-1} + Current{0,0,0,1} = {2,1,-3,0} = Power.
func (d Dimension) Mul(other Dimension) Dimension {
	return Dimension{
		L: d.L + other.L,
		M: d.M + other.M,
		T: d.T + other.T,
		I: d.I + other.I,
		K: d.K + other.K,
	}
}

// Div returns the dimension of a quotient: exponents subtract.
//
// Voltage / Current = {2,1,-3,-1} - {0,0,0,1} = {2,1,-3,-2} = Impedance.
func (d Dimension) Div(other Dimension) Dimension {
	return Dimension{
		L: d.L - other.L,
		M: d.M - other.M,
		T: d.T - other.T,
		I: d.I - other.I,
		K: d.K - other.K,
	}
}

// Reciprocal returns the dimension of 1/d.
func (d Dimension) Reciprocal() Dimension {
	return Dimensionless.Div(d)
}

// IsDimensionless reports whether every exponent is zero.
func (d Dimension) IsDimensionless() bool {
	return d == Dimensionless
}

// String renders the dimension in exponent form, e.g. "L^2 M T^-3 I^-1".
// Dimensionless renders as "1". Intended for error messages, which is where
// dimensional mismatches are diagnosed.
func (d Dimension) String() string {
	if d.IsDimensionless() {
		return "1"
	}

	out := ""
	for _, term := range []struct {
		symbol   string
		exponent int8
	}{
		{"L", d.L}, {"M", d.M}, {"T", d.T}, {"I", d.I}, {"K", d.K},
	} {
		if term.exponent == 0 {
			continue
		}

		if out != "" {
			out += " "
		}

		out += term.symbol
		if term.exponent != 1 {
			out += "^" + strconv.Itoa(int(term.exponent))
		}
	}

	return out
}
