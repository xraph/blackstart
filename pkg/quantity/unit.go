package quantity

import "fmt"

// Kind distinguishes quantities that share a dimension but not a meaning.
//
// Active, reactive and apparent power are all kg m^2 s^-3. Dimensional
// analysis alone will happily add megawatts to megavars, which is a real and
// common bug. Kind is what makes that an error.
//
// Kind is deliberately small. It exists for cases where physics genuinely
// cannot distinguish two quantities that engineers must never confuse — not as
// a general-purpose tagging mechanism.
type Kind uint8

const (
	// KindGeneric is the default: the dimension fully describes the quantity.
	KindGeneric Kind = iota

	// KindActive is real power, P, in watts.
	KindActive

	// KindReactive is reactive power, Q, in volt-amperes reactive.
	KindReactive

	// KindApparent is apparent power, S, in volt-amperes.
	KindApparent
)

// String renders the kind for error messages.
func (k Kind) String() string {
	switch k {
	case KindGeneric:
		return "generic"
	case KindActive:
		return "active"
	case KindReactive:
		return "reactive"
	case KindApparent:
		return "apparent"
	default:
		return fmt.Sprintf("kind(%d)", uint8(k))
	}
}

// Unit is a named scale on a dimension.
//
// A Unit is meaningful without a magnitude, which is what lets the Formula
// engine's dimensional checker reason about an expression before evaluating
// it.
type Unit struct {
	// Symbol is the conventional written form, e.g. "MW", "kV", "pu".
	Symbol string

	// Dim is the physical dimension. Per-unit units carry the dimension of
	// the quantity they scale: a per-unit voltage has dimension Voltage.
	Dim Dimension

	// Kind distinguishes dimensionally-identical quantities.
	Kind Kind

	// ToSI converts a magnitude in this unit to the SI base unit by
	// multiplication. Meaningless, and left zero, when PerUnit is true.
	ToSI float64

	// PerUnit marks a normalized value whose absolute meaning depends on a
	// reference Base. Conversion to SI requires that base, and attempting it
	// without one is an error rather than a silent wrong answer. This is the
	// single most important field in the package.
	PerUnit bool
}

// Dimensionless units: ratios, angles, and power factor.
//
//nolint:gochecknoglobals // a fixed physical vocabulary, not mutable state
var (
	Ratio       = Unit{Symbol: "", Dim: Dimensionless, ToSI: 1}
	Percent     = Unit{Symbol: "%", Dim: Dimensionless, ToSI: 0.01}
	Radian      = Unit{Symbol: "rad", Dim: Dimensionless, ToSI: 1}
	Degree      = Unit{Symbol: "deg", Dim: Dimensionless, ToSI: degToRad}
	PowerFactor = Unit{Symbol: "pf", Dim: Dimensionless, ToSI: 1}
)

// Voltage and current units. SI bases are the volt and the ampere.
//
//nolint:gochecknoglobals // a fixed physical vocabulary, not mutable state
var (
	Volt       = Unit{Symbol: "V", Dim: Voltage, ToSI: 1}
	Kilovolt   = Unit{Symbol: "kV", Dim: Voltage, ToSI: 1e3}
	Ampere     = Unit{Symbol: "A", Dim: Current, ToSI: 1}
	Kiloampere = Unit{Symbol: "kA", Dim: Current, ToSI: 1e3}
)

// Power units. The three families are dimensionally identical and are
// separated by Kind, which is the entire reason Kind exists: without it,
// adding megawatts to megavars type-checks.
//
//nolint:gochecknoglobals // a fixed physical vocabulary, not mutable state
var (
	Watt     = Unit{Symbol: "W", Dim: Power, Kind: KindActive, ToSI: 1}
	Kilowatt = Unit{Symbol: "kW", Dim: Power, Kind: KindActive, ToSI: 1e3}
	Megawatt = Unit{Symbol: "MW", Dim: Power, Kind: KindActive, ToSI: 1e6}

	Var     = Unit{Symbol: "VAr", Dim: Power, Kind: KindReactive, ToSI: 1}
	Kilovar = Unit{Symbol: "kVAr", Dim: Power, Kind: KindReactive, ToSI: 1e3}
	Megavar = Unit{Symbol: "MVAr", Dim: Power, Kind: KindReactive, ToSI: 1e6}

	VoltAmpere     = Unit{Symbol: "VA", Dim: Power, Kind: KindApparent, ToSI: 1}
	KilovoltAmpere = Unit{Symbol: "kVA", Dim: Power, Kind: KindApparent, ToSI: 1e3}
	MegavoltAmpere = Unit{Symbol: "MVA", Dim: Power, Kind: KindApparent, ToSI: 1e6}
)

// Energy, impedance, admittance and frequency units.
//
//nolint:gochecknoglobals // a fixed physical vocabulary, not mutable state
var (
	Joule        = Unit{Symbol: "J", Dim: Energy, ToSI: 1}
	KilowattHour = Unit{Symbol: "kWh", Dim: Energy, ToSI: 3.6e6}
	MegawattHour = Unit{Symbol: "MWh", Dim: Energy, ToSI: 3.6e9}

	Ohm     = Unit{Symbol: "ohm", Dim: Impedance, ToSI: 1}
	Kilohm  = Unit{Symbol: "kohm", Dim: Impedance, ToSI: 1e3}
	Siemens = Unit{Symbol: "S", Dim: Admittance, ToSI: 1}

	Hertz = Unit{Symbol: "Hz", Dim: Frequency, ToSI: 1}
)

// Length, time and temperature units, for span geometry, durations, and the
// IEEE 738 thermal model.
//
// Celsius is an affine scale rather than a multiplicative one, so it carries
// ToSI 1 and is converted explicitly in convert.go. A ToSI factor cannot
// express the 273.15 offset, and pretending otherwise would make 0 degC
// convert to 0 K.
//
//nolint:gochecknoglobals // a fixed physical vocabulary, not mutable state
var (
	Meter     = Unit{Symbol: "m", Dim: Length, ToSI: 1}
	Kilometer = Unit{Symbol: "km", Dim: Length, ToSI: 1e3}

	Second = Unit{Symbol: "s", Dim: Time, ToSI: 1}
	Minute = Unit{Symbol: "min", Dim: Time, ToSI: 60}
	Hour   = Unit{Symbol: "h", Dim: Time, ToSI: 3600}

	Kelvin  = Unit{Symbol: "K", Dim: Temperature, ToSI: 1}
	Celsius = Unit{Symbol: "degC", Dim: Temperature, ToSI: 1}
)

// Per-unit units. Each carries the dimension of the quantity it normalizes, so
// dimensional checking still works on per-unit arithmetic, but none can be
// converted to SI without a Base.
//
//nolint:gochecknoglobals // a fixed physical vocabulary, not mutable state
var (
	PerUnitVoltage    = Unit{Symbol: "pu", Dim: Voltage, PerUnit: true}
	PerUnitCurrent    = Unit{Symbol: "pu", Dim: Current, PerUnit: true}
	PerUnitActive     = Unit{Symbol: "pu", Dim: Power, Kind: KindActive, PerUnit: true}
	PerUnitReactive   = Unit{Symbol: "pu", Dim: Power, Kind: KindReactive, PerUnit: true}
	PerUnitApparent   = Unit{Symbol: "pu", Dim: Power, Kind: KindApparent, PerUnit: true}
	PerUnitImpedance  = Unit{Symbol: "pu", Dim: Impedance, PerUnit: true}
	PerUnitAdmittance = Unit{Symbol: "pu", Dim: Admittance, PerUnit: true}
)

const degToRad = 0.017453292519943295 // pi / 180

// Compatible reports whether two units describe the same physical quantity and
// may therefore be added, subtracted or compared.
//
// Both the dimension and the kind must match. Per-unit-ness must also match:
// a per-unit voltage and a kilovolt are the same dimension and the same kind,
// but adding them without resolving the base is precisely the bug this package
// exists to prevent.
func (u Unit) Compatible(other Unit) bool {
	return u.Dim == other.Dim && u.Kind == other.Kind && u.PerUnit == other.PerUnit
}

// String renders the unit symbol, or "1" for a bare ratio.
func (u Unit) String() string {
	if u.Symbol == "" {
		return "1"
	}

	if u.PerUnit {
		return "pu(" + u.Dim.String() + ")"
	}

	return u.Symbol
}

// SIUnitFor returns the SI base unit for a dimension and kind. It is the
// target of conversions and the unit in which quantities are persisted.
func SIUnitFor(dim Dimension, kind Kind) (Unit, error) {
	for _, u := range []Unit{
		Volt, Ampere, Watt, Var, VoltAmpere, Joule, Ohm, Siemens,
		Hertz, Meter, Second, Kelvin, Ratio,
	} {
		if u.Dim == dim && u.Kind == kind && u.ToSI == 1 {
			return u, nil
		}
	}

	return Unit{}, fmt.Errorf(
		"quantity: no SI unit registered for dimension %s kind %s", dim, kind)
}
