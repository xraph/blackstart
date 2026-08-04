// Package id declares the weather extension's identifier prefixes.
package id

import "github.com/xraph/blackstart/pkg/id"

// Owner is the extension these prefixes belong to.
const Owner = "weather"

// Identifier prefixes. Immutable once shipped.
const (
	// Cycle is one run of one model — a single issuance of HRRR or GFS.
	Cycle id.Prefix = "wxcyc"

	// Grid is one retrieved GRIB2 message set: one field, at one forecast
	// hour, of one cycle.
	//
	// The unit a dataset version is recorded against, because it is the unit
	// actually fetched. HRRR publishes an index sidecar giving the byte offset
	// of every message, so a field is pulled by range out of a much larger
	// file — and the hash then covers what was read rather than what was
	// published.
	Grid id.Prefix = "wxgrd"

	// Station is a surface observing site.
	Station id.Prefix = "wxstn"
)

//nolint:gochecknoinits // prefix registration is init-time by design
func init() {
	for _, p := range []struct {
		prefix id.Prefix
		name   string
	}{
		{Cycle, "Cycle"},
		{Grid, "Grid"},
		{Station, "Station"},
	} {
		id.MustRegister(p.prefix, Owner, p.name)
	}
}

// NewCycle mints a cycle identifier.
func NewCycle() id.ID { return id.MustNew(Cycle) }

// NewGrid mints a grid identifier.
func NewGrid() id.ID { return id.MustNew(Grid) }

// NewStation mints a station identifier.
func NewStation() id.ID { return id.MustNew(Station) }

// ParseCycle reads a cycle identifier, rejecting any other type.
func ParseCycle(s string) (id.ID, error) { return id.ParseWithPrefix(s, Cycle) }

// ParseGrid reads a grid identifier, rejecting any other type.
func ParseGrid(s string) (id.ID, error) { return id.ParseWithPrefix(s, Grid) }

// ParseStation reads a station identifier, rejecting any other type.
func ParseStation(s string) (id.ID, error) { return id.ParseWithPrefix(s, Station) }
