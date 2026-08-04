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

	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/geo/shape"
	nmid "github.com/xraph/blackstart/extensions/networkmodel/id"
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

	// Routed geometry follows a real right-of-way whose structures are not
	// known. The path came from a source that digitised where the line runs —
	// EIA's republication of the HIFLD transmission layer, a state ArcGIS
	// mirror — and the structures along it were interpolated at a nominal span.
	//
	// A third value rather than a shade of the other two, because it is a
	// different claim from either. Calling it Surveyed asserts structures
	// nobody recorded; calling it Interpolated discards a real right-of-way and
	// implies a great circle that ignores the ridge the line actually crosses.
	//
	// The distinction is measured, not assumed. Vertex spacing in that layer
	// has a median near a real span but a maximum over 40 km, which no
	// conductor crosses unsupported — see the phase-3 data survey, §1.4.1. The
	// path is real; the structures are not.
	Routed Provenance = "routed"

	// Interpolated geometry is a straight great-circle path between two known
	// endpoints, broken at a nominal span length. The structures are imaginary
	// and evenly spaced, and so is the path.
	Interpolated Provenance = "interpolated"
)

// Valid reports whether this is a provenance the model knows.
func (p Provenance) Valid() bool {
	return p == Surveyed || p == Routed || p == Interpolated
}

// StructuresAreReal reports whether the structures along this route were
// recorded rather than invented.
//
// The question a per-span hazard needs answered. A span between two real
// structures is a physical object with a length and a bearing; a span between
// two interpolated ones is a bookkeeping device, and a failure probability
// conditioned on it means something weaker. Callers that must not conflate the
// two ask here rather than comparing against a literal.
func (p Provenance) StructuresAreReal() bool {
	return p == Surveyed
}

// PathIsReal reports whether the route follows a digitised right-of-way.
//
// True for both Surveyed and Routed. Weather sampled along the path is honest
// in both cases even where the structure positions are not, which is why this
// is a separate question from StructuresAreReal.
func (p Provenance) PathIsReal() bool {
	return p == Surveyed || p == Routed
}

// CarriesElectrically reports whether a carrier can be solved.
//
// A branch in a network model has an impedance and a rating; a circuit has
// neither, by design — see the real-circuit-identity design, §3.1. Anything
// that runs a power flow or a contingency asks this before it starts, so the
// refusal names the reason rather than surfacing as a missing branch deep in a
// solver.
func CarriesElectrically(carrier id.ID) bool {
	return carrier.Prefix() == nmid.Model
}

// Route is a path over the ground, and what it carries.
//
// The json tags matter beyond tidiness: these types cross the HTTP boundary,
// and every other response in this platform is snake_case. A single camelCase
// island is the kind of inconsistency a client library papers over once and
// then depends on forever.
type Route struct {
	// ID identifies this route.
	ID id.ID `json:"id"`

	// CarrierID names what this path carries: a branch in a network model, or
	// a real circuit. Exactly one of the two, and the identifier's own prefix
	// says which — a second discriminator field could disagree with it.
	//
	// A route belongs to one carrier; a carrier has one route.
	CarrierID id.ID `json:"carrier_id"`

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

	if r.CarrierID.IsZero() {
		return fmt.Errorf(
			"%w: route %s names no carrier; a path over the ground that carries "+
				"nothing is not part of the network", bserr.ErrInvalid, r.ID)
	}

	// The prefix is the discriminator, so an unrecognised one is not a
	// harmless extra case: it is a route whose carrier nothing can resolve,
	// and it would store, render and read back as complete.
	if !CarriesElectrically(r.CarrierID) && r.CarrierID.Prefix() != gid.Circuit {
		return fmt.Errorf(
			"%w: route %s carries %s, which is neither a branch in a network "+
				"model nor a circuit; nothing could say what runs along it",
			bserr.ErrInvalid, r.ID, r.CarrierID)
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

// Circuit is a transmission line somebody actually built.
//
// Distinct from a network model's branch, and the distinction is the point. An
// ACTIVSg branch is a number in a synthetic case that corresponds to no real
// line anywhere; a circuit is a line that exists, imported from a source that
// recorded where it runs. Geometry from OpenStreetMap or a transmission line
// layer carries a circuit, and attaching it to a synthetic branch would claim
// the two are the same conductor.
//
// A circuit carries no electrical parameters, deliberately. The moment it has
// an impedance somebody will solve it, and the impedance will be a guess. A
// real line whose electrical parameters are known is a branch in a network
// model, which is a different import from a different source.
type Circuit struct {
	// ID identifies this circuit.
	ID id.ID `json:"id"`

	// ExternalID is the identifier the source used: an OSM way, a feature id.
	ExternalID string `json:"external_id"`

	// Source names where it came from — "openstreetmap", a feature service URL.
	//
	// Together with ExternalID this is the reconciliation key, which is what
	// makes an import re-runnable: the same way seen twice updates one circuit
	// rather than creating a second.
	Source string `json:"source"`

	// VoltageKV is the nominal voltage, zero when the source did not say.
	VoltageKV float64 `json:"voltage_kv"`

	// Operator is who runs it, empty when unknown.
	Operator string `json:"operator"`
}

// Validate reports whether a circuit can be stored.
func (c Circuit) Validate() error {
	if c.ID.IsZero() {
		return fmt.Errorf("%w: a circuit needs an identifier", bserr.ErrInvalid)
	}

	if c.ExternalID == "" || c.Source == "" {
		return fmt.Errorf(
			"%w: circuit %s has no external identifier and source; without them "+
				"nothing can be traced back to what it was derived from, and a "+
				"re-import cannot tell it from a new line", bserr.ErrInvalid, c.ID)
	}

	return nil
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
