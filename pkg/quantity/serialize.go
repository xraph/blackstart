package quantity

import (
	"fmt"
	"strconv"
	"strings"
)

// This file defines the canonical written form of a unit and a base, and the
// read half of persistence. Everything that serializes a Quantity — the API,
// the formula engine, an error message — goes through UnitName, so a value
// means the same thing wherever it surfaces.
//
// On storage: quantities persist as a plain SI DOUBLE PRECISION column, never
// JSONB. A numeric column is indexable, aggregatable, and readable from psql
// by a researcher who has never seen this code. There is deliberately no
// driver.Valuer on Quantity, because a Valuer would have to guess a unit; the
// store mapper calls MustSI on write and FromSI on read.

// FromSI builds a quantity from a value in SI base units, expressed in target.
//
// This is the read half of persistence: the column holds watts, the model
// wants megawatts.
func FromSI(si float64, target Unit) (Quantity, error) {
	if target.PerUnit {
		return Quantity{}, fmt.Errorf(
			"quantity: FromSI needs an absolute unit, got %s; "+
				"normalize with ToPerUnit after construction", target)
	}

	if target == Celsius {
		return Quantity{mag: si - kelvinOffset, unit: target}, nil
	}

	return Quantity{mag: si / target.ToSI, unit: target}, nil
}

// UnitName returns the canonical spelling used in JSON, DTL and error text.
//
// Absolute units use their symbol. Per-unit units are written "pu:" followed
// by the symbol of the SI unit they normalize — "pu:V", "pu:W", "pu:ohm" — so
// that a per-unit voltage and a per-unit impedance stay distinguishable in a
// serialized form. Unit.String() is not used here because its per-unit
// rendering shows the raw exponent vector, which is right for a debug message
// and wrong for something a user reads and writes.
func UnitName(u Unit) string {
	if !u.PerUnit {
		if u.Symbol == "" {
			return "1"
		}

		return u.Symbol
	}

	si, err := SIUnitFor(u.Dim, u.Kind)
	if err != nil {
		return "pu:?"
	}

	return "pu:" + si.Symbol
}

// allUnits is the reverse-lookup table for UnitName. Its contents must stay in
// step with the vocabulary in unit.go; a test asserts the round trip.
//
//nolint:gochecknoglobals // a fixed lookup table, not mutable state
var allUnits = []Unit{
	Ratio, Percent, Radian, Degree, PowerFactor,
	Volt, Kilovolt, Ampere, Kiloampere,
	Watt, Kilowatt, Megawatt,
	Var, Kilovar, Megavar,
	VoltAmpere, KilovoltAmpere, MegavoltAmpere,
	Joule, KilowattHour, MegawattHour,
	Ohm, Kilohm, Siemens, Hertz,
	Meter, Kilometer, Second, Minute, Hour,
	Kelvin, Celsius,
	PerUnitVoltage, PerUnitCurrent, PerUnitActive, PerUnitReactive,
	PerUnitApparent, PerUnitImpedance, PerUnitAdmittance,
}

// ParseUnit resolves a canonical unit name produced by UnitName.
func ParseUnit(name string) (Unit, error) {
	for _, u := range allUnits {
		if UnitName(u) == name {
			return u, nil
		}
	}

	return Unit{}, fmt.Errorf("quantity: unknown unit %q", name)
}

// FormatBase renders a base in a form that round-trips exactly, or "" for an
// absolute quantity. Magnitudes use strconv's shortest round-tripping form.
func FormatBase(b *Base) string {
	if b == nil {
		return ""
	}

	return strconv.FormatFloat(b.sBase/1e6, 'g', -1, 64) + "MVA/" +
		strconv.FormatFloat(b.vBase/1e3, 'g', -1, 64) + "kV"
}

// ParseBase reads the form produced by FormatBase. An empty string yields nil,
// which is the correct base for an absolute quantity.
func ParseBase(s string) (*Base, error) {
	if s == "" {
		return nil, nil //nolint:nilnil // nil base is meaningful, not an absence
	}

	mvaPart, kvPart, ok := strings.Cut(s, "/")
	if !ok {
		return nil, fmt.Errorf("quantity: malformed base %q, want e.g. 100MVA/138kV", s)
	}

	mva, err := strconv.ParseFloat(strings.TrimSuffix(mvaPart, "MVA"), 64)
	if err != nil {
		return nil, fmt.Errorf("quantity: malformed apparent power base in %q: %w", s, err)
	}

	kv, err := strconv.ParseFloat(strings.TrimSuffix(kvPart, "kV"), 64)
	if err != nil {
		return nil, fmt.Errorf("quantity: malformed voltage base in %q: %w", s, err)
	}

	return NewBase(mva, kv)
}
