// Package asset is where the network physically is.
//
// Separate from topology because the two answer different questions and change
// on different schedules: the electrical model says what is connected to what,
// and this says where it stands. A line's impedance does not change when a
// survey corrects its route by fifty meters, and a re-conductoring changes the
// impedance without moving a single structure.
//
// Geometry lives per span rather than one polyline per branch. That is the
// whole reason this package exists in a project about weather-driven outage
// risk: a conductor breaks between two structures, an ice load is local, and a
// 200 km line passes through several weather systems at once. One geometry per
// branch would force every per-span probability to condition on the same
// weather, which is the modelling error the platform is built to avoid.
package asset

import (
	"fmt"

	"github.com/xraph/blackstart/extensions/geo/shape"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Provenance says how a geometry was arrived at.
//
// Recorded on the row rather than inferred from a dataset version, because the
// distinction changes what a result means and has to be visible wherever the
// geometry is: a per-span failure probability conditioned on a straight-line
// guess is not the same claim as one conditioned on a surveyed route, and
// nothing downstream should have to join through a provenance table to find
// that out.
type Provenance string

const (
	// Surveyed geometry comes from a source that recorded where the structures
	// actually are.
	Surveyed Provenance = "surveyed"

	// Interpolated geometry is a straight great-circle path between two known
	// endpoints, broken at a nominal span length. The structures are imaginary
	// and evenly spaced.
	Interpolated Provenance = "interpolated"
)

// Valid reports whether this is a provenance the model knows.
func (p Provenance) Valid() bool {
	return p == Surveyed || p == Interpolated
}

// Route is a branch's path over the ground.
//
// The json tags matter beyond tidiness: these types cross the HTTP boundary,
// and every other response in this platform is snake_case. A single camelCase
// island is the kind of inconsistency a client library papers over once and
// then depends on forever.
type Route struct {
	// ID identifies this route.
	ID id.ID `json:"id"`

	// BranchID names the electrical branch it carries. A route belongs to one
	// branch; a branch has one route.
	BranchID id.ID `json:"branch_id"`

	// Provenance says whether the path was surveyed or interpolated.
	Provenance Provenance `json:"provenance"`

	// LengthM is the summed length of its spans, in meters.
	//
	// Stored rather than recomputed on read, because it is what a per-unit
	// impedance is checked against and a report should not depend on whether
	// the geometry happened to be loaded.
	LengthM float64 `json:"length_m"`
}

// Validate reports whether a route can be stored.
func (r Route) Validate() error {
	if r.ID.IsZero() {
		return fmt.Errorf("%w: a route needs an identifier", bserr.ErrInvalid)
	}

	if r.BranchID.IsZero() {
		return fmt.Errorf(
			"%w: route %s names no branch; a path over the ground that carries "+
				"nothing is not part of the network", bserr.ErrInvalid, r.ID)
	}

	if !r.Provenance.Valid() {
		return fmt.Errorf(
			"%w: route %s has provenance %q; a geometry that cannot say whether "+
				"it was surveyed or guessed cannot be conditioned on",
			bserr.ErrInvalid, r.ID, r.Provenance)
	}

	return nil
}

// Span is one segment of a route, between two structures.
type Span struct {
	// ID identifies this span.
	ID id.ID `json:"id"`

	// RouteID names the route it belongs to.
	RouteID id.ID `json:"route_id"`

	// Sequence orders spans along the route from its first end.
	Sequence int `json:"sequence"`

	// Geometry is the path between the two structures.
	Geometry shape.Line `json:"geometry"`

	// LengthM is its great-circle length in meters.
	LengthM float64 `json:"length_m"`

	// AzimuthDeg is the initial bearing, clockwise from true north.
	//
	// Stored because it is what a wind loading is resolved against: a
	// crosswind on a span is the component perpendicular to it, and a span
	// aligned with the wind carries almost none of it.
	AzimuthDeg float64 `json:"azimuth_deg"`
}

// Validate reports whether a span can be stored.
func (s Span) Validate() error {
	if s.ID.IsZero() || s.RouteID.IsZero() {
		return fmt.Errorf("%w: a span needs both identifiers", bserr.ErrInvalid)
	}

	return s.Geometry.Validate()
}

// Structure is a tower or pole.
type Structure struct {
	// ID identifies this structure.
	ID id.ID `json:"id"`

	// Location is where it stands.
	Location shape.Point `json:"location"`

	// HeightM is its height above ground in meters. Zero when unknown.
	HeightM float64 `json:"height_m"`

	// Kind is what it is: "lattice", "monopole", "wood". Free text, because
	// the vocabularies differ per source and forcing them into one enumeration
	// would discard the distinction the source drew.
	Kind string `json:"kind"`
}

// Footprint is the area an entity occupies.
type Footprint struct {
	// ID identifies this footprint.
	ID id.ID `json:"id"`

	// EntityID names what it belongs to — usually a substation.
	EntityID id.ID `json:"entity_id"`

	// Boundary is the outline, closed by repeating its first point.
	Boundary shape.Line `json:"boundary"`
}
