// Package conductor is what a transmission line is physically made of, and
// how confident this platform is about that.
//
// IEEE 738 needs a conductor's diameter, its resistance at two temperatures,
// its emissivity and absorptivity, and the temperature above which it must not
// be operated. Nothing else in this platform has any of them: the network
// model carries a single static rating per branch, and the asset extension the
// project spec assigns physical properties to has not been built.
//
// So this ships a library of standard ACSR types. That is published
// engineering reference data — the same category as the earth radius the GRIB
// decoder hardcodes — rather than a dataset consumed under spec §8, so no
// registry entry is involved. When asset exists, the library moves there.
//
// The library is the easy half. The hard half is [Assignment]: deciding which
// of these a given branch actually is. See its documentation.
package conductor

import (
	"fmt"
	"slices"
	"strings"

	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/quantity"
)

// Conductor is one type of overhead conductor.
//
// Every field is in SI, because the models that consume them are, and a
// library carrying inches and circular mils would put a conversion at every
// call site rather than in one place.
type Conductor struct {
	// Code is the short handle: "drake". Stable, because assignments store it.
	Code string `json:"code"`

	// Name is how a catalog writes it.
	Name string `json:"name"`

	// DiameterM is the overall diameter, in meters. The face the wind pushes
	// on and the surface heat leaves from, so it appears in every model here.
	DiameterM float64 `json:"diameter_m"`

	// AreaMM2 is the aluminum cross-section, recorded because it is how
	// conductors are named and compared even though nothing here computes
	// with it.
	AreaMM2 float64 `json:"area_mm2"`

	// ResistanceLowOhmPerM and ResistanceHighOhmPerM are AC resistance at
	// LowTempC and HighTempC.
	//
	// Two points rather than a coefficient, because that is how IEEE 738 and
	// every conductor table state it, and converting to a coefficient here
	// would mean this library disagreed with its own sources in the last
	// digit.
	ResistanceLowOhmPerM  float64 `json:"resistance_low_ohm_per_m"`
	ResistanceHighOhmPerM float64 `json:"resistance_high_ohm_per_m"`
	LowTempC              float64 `json:"low_temp_c"`
	HighTempC             float64 `json:"high_temp_c"`

	// Emissivity is how readily the surface radiates heat away, and
	// Absorptivity how readily it takes heat from the sun. Both 0 to 1.
	//
	// They move together as a conductor weathers — a new bright conductor is
	// low in both, an aged dark one high in both — which is why one figure is
	// conventionally used for each and why 0.5 is the usual middle.
	Emissivity   float64 `json:"emissivity"`
	Absorptivity float64 `json:"absorptivity"`

	// MaxTempC is the highest temperature this conductor may be operated at.
	// The rating is the current that brings it exactly here.
	MaxTempC float64 `json:"max_temp_c"`
}

// Validate reports whether a conductor is usable.
func (c Conductor) Validate() error {
	if strings.TrimSpace(c.Code) == "" {
		return fmt.Errorf("%w: a conductor needs a code", bserr.ErrInvalid)
	}

	if c.DiameterM <= 0 {
		return fmt.Errorf(
			"%w: conductor %q has diameter %v; every model here divides or "+
				"multiplies by it", bserr.ErrInvalid, c.Code, c.DiameterM)
	}

	if c.ResistanceLowOhmPerM <= 0 || c.ResistanceHighOhmPerM <= 0 {
		return fmt.Errorf(
			"%w: conductor %q has a non-positive resistance", bserr.ErrInvalid, c.Code)
	}

	if c.HighTempC <= c.LowTempC {
		return fmt.Errorf(
			"%w: conductor %q tabulates resistance at %v C and %v C; the two "+
				"points must differ or the interpolation divides by zero",
			bserr.ErrInvalid, c.Code, c.LowTempC, c.HighTempC)
	}

	if c.Emissivity <= 0 || c.Emissivity > 1 ||
		c.Absorptivity <= 0 || c.Absorptivity > 1 {
		return fmt.Errorf(
			"%w: conductor %q has emissivity %v and absorptivity %v; both are "+
				"fractions of unity", bserr.ErrInvalid,
			c.Code, c.Emissivity, c.Absorptivity)
	}

	if c.MaxTempC <= 0 {
		return fmt.Errorf(
			"%w: conductor %q has no maximum operating temperature, so there is "+
				"nothing for a rating to be the current at",
			bserr.ErrInvalid, c.Code)
	}

	return nil
}

// ResistanceAt is the conductor's AC resistance at a temperature, in ohms per
// meter.
//
// Linear between the two tabulated points, which is what IEEE 738 assumes over
// the range a conductor operates in.
//
// Extrapolation past them is deliberate rather than guarded. A rating exists to
// answer "how much current before this conductor exceeds its limit", and
// clamping at the limit would make the answer depend on which side of it the
// arithmetic happened to land.
func (c Conductor) ResistanceAt(tempC float64) float64 {
	slope := (c.ResistanceHighOhmPerM - c.ResistanceLowOhmPerM) /
		(c.HighTempC - c.LowTempC)

	return c.ResistanceLowOhmPerM + slope*(tempC-c.LowTempC)
}

// ResistanceUnit is what ResistanceAt returns, for a caller assembling a
// typed quantity.
//
// Ohms per meter has no entry in the quantity vocabulary and does not warrant
// one: it appears in exactly one expression, inside IEEE 738's heat balance,
// and never crosses a boundary where it could be added to something else.
func ResistanceUnit() quantity.Unit { return quantity.Ohm }

// library is every conductor this platform knows.
//
// Standard ACSR types covering transmission practice, with the properties IEEE
// 738 tabulates. Emissivity and absorptivity are 0.5 throughout — the value
// the standard's own worked example uses, and the conventional middle for
// weathered conductor.
//
// Drake's row is the one IEEE 738-2012's worked example is built on, which
// makes it the row the thermal package's reference test checks against. It
// should not be edited without re-running that test.
//
//nolint:gochecknoglobals // a fixed reference table, not mutable state
var library = []Conductor{
	{
		Code: "drake", Name: "ACSR 795 kcmil 26/7 Drake",
		DiameterM: 0.02814, AreaMM2: 402.8,
		ResistanceLowOhmPerM: 7.283e-5, ResistanceHighOhmPerM: 8.688e-5,
		LowTempC: 25, HighTempC: 75,
		Emissivity: 0.5, Absorptivity: 0.5, MaxTempC: 100,
	},
	{
		Code: "cardinal", Name: "ACSR 954 kcmil 54/7 Cardinal",
		DiameterM: 0.03038, AreaMM2: 483.4,
		ResistanceLowOhmPerM: 6.135e-5, ResistanceHighOhmPerM: 7.310e-5,
		LowTempC: 25, HighTempC: 75,
		Emissivity: 0.5, Absorptivity: 0.5, MaxTempC: 100,
	},
	{
		Code: "rail", Name: "ACSR 954 kcmil 45/7 Rail",
		DiameterM: 0.02959, AreaMM2: 483.4,
		ResistanceLowOhmPerM: 6.156e-5, ResistanceHighOhmPerM: 7.336e-5,
		LowTempC: 25, HighTempC: 75,
		Emissivity: 0.5, Absorptivity: 0.5, MaxTempC: 100,
	},
	{
		Code: "hawk", Name: "ACSR 477 kcmil 26/7 Hawk",
		DiameterM: 0.02179, AreaMM2: 241.7,
		ResistanceLowOhmPerM: 1.204e-4, ResistanceHighOhmPerM: 1.437e-4,
		LowTempC: 25, HighTempC: 75,
		Emissivity: 0.5, Absorptivity: 0.5, MaxTempC: 100,
	},
}

// Library returns every conductor, in a stable order.
//
// A fresh slice each call: callers hold the result, and a shared backing array
// would let one of them reorder the library for everybody.
func Library() []Conductor { return slices.Clone(library) }

// ByCode finds one conductor.
//
// # Errors
//
// Returns an error wrapping bserr.ErrNotFound when no entry has that code.
func ByCode(code string) (Conductor, error) {
	for _, got := range library {
		if got.Code == code {
			return got, nil
		}
	}

	return Conductor{}, fmt.Errorf(
		"%w: no conductor %q in the library", bserr.ErrNotFound, code)
}
