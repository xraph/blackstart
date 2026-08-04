// Package model names the numerical weather prediction models this platform
// consumes.
//
// Its own package because both the field vocabulary and the forecast types
// need it, and the field vocabulary is what the forecast types are built from.
// Putting the model identity in either of those would make the other import
// it, and the resulting cycle is not one Go permits.
package model

// Model names a numerical weather prediction model.
//
// A closed set, like the source registry it draws from. Adding a model is a
// decision about which public data this platform consumes — spec §8 makes it
// one that needs approval — and a free-text field would let one arrive
// unnoticed.
type Model string

const (
	// HRRR is NOAA's High-Resolution Rapid Refresh: 3 km, CONUS only, issued
	// hourly. The CONUS-only part matters downstream: a span outside its
	// domain has no sample rather than a zero.
	HRRR Model = "hrrr"

	// GFS is NOAA's Global Forecast System: 0.25 degrees, global, issued four
	// times daily. The fallback where HRRR does not reach, and the longer
	// horizon where it does.
	GFS Model = "gfs"
)

// All returns every model in the set.
func All() []Model { return []Model{HRRR, GFS} }

// Valid reports whether this is a model the platform knows.
func (m Model) Valid() bool { return m == HRRR || m == GFS }

// String renders the model name.
func (m Model) String() string { return string(m) }
