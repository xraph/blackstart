// Package source reads transmission geometry published by other people.
//
// Two shapes of source, and the difference decides what the imported route may
// claim. A line layer — EIA's republication of the HIFLD transmission data, a
// state ArcGIS mirror — publishes polylines whose vertices are digitizing
// points; the right-of-way is real and the structures are not. OpenStreetMap
// publishes ways whose nodes are the towers themselves.
//
// Both are parsed into the same Line, and the presence of Structures is what
// tells geo.Import which claim it may make. Nothing here decides that: a
// parser reports what the source contained.
package source

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/blackstart/extensions/geo/shape"
)

// Fetcher reads lines covering an area.
//
// Two implementations and the difference between them is not an interface
// detail: Overpass publishes structures and a line layer does not, so the same
// call against the two produces geometry that supports different claims. The
// interface exists so a caller can be given one of them, not so the two can be
// treated as equivalent.
type Fetcher interface {
	// Fetch reads every line intersecting the box, at most limit of them.
	Fetch(ctx context.Context, box BoundingBox, limit int) ([]Line, error)
}

// Line is one circuit as a source published it.
type Line struct {
	// ExternalID is the identifier the source used.
	//
	// Kept because reconciliation has to stay auditable: a route imported from
	// OSM way 318827441 should be traceable back to that way, and a source
	// identifier is never discarded once something has been derived from it.
	ExternalID string

	// Source names where it came from, for the provenance record.
	Source string

	// Path is the digitized right-of-way, in order.
	Path shape.Line

	// Structures are recorded structure positions, in order, when the source
	// publishes them. Empty for a line layer.
	Structures shape.Line

	// Voltage is the nominal voltage in kilovolts, zero when the source did
	// not say. Not used for geometry; carried so an importer can filter to the
	// transmission classes it wants rather than pulling every distribution
	// tap in a bounding box.
	VoltageKV float64

	// Owner is the operator the source named, empty when unknown.
	Owner string
}

// Validate reports whether a parsed line can be imported.
func (l Line) Validate() error {
	if l.ExternalID == "" {
		return errors.New(
			"source: a line with no external identifier cannot be traced back " +
				"to what it was derived from")
	}

	if err := l.Path.Validate(); err != nil {
		return fmt.Errorf("source: line %s: %w", l.ExternalID, err)
	}

	if len(l.Structures) > 0 {
		if err := l.Structures.Validate(); err != nil {
			return fmt.Errorf("source: line %s structures: %w", l.ExternalID, err)
		}
	}

	return nil
}

// BoundingBox is the area a fetch covers, in degrees.
//
// Every source here is national or global and none of this project's work is.
// Fetching a whole continent to draw one storm is slow for the caller and rude
// to a public endpoint, so a box is required rather than optional.
type BoundingBox struct {
	MinLon, MinLat, MaxLon, MaxLat float64
}

// Validate reports whether the box describes an area on the earth.
func (b BoundingBox) Validate() error {
	if b.MinLon >= b.MaxLon || b.MinLat >= b.MaxLat {
		return fmt.Errorf(
			"source: bounding box %v has no area; min must be below max on both axes", b)
	}

	if b.MinLon < -180 || b.MaxLon > 180 || b.MinLat < -90 || b.MaxLat > 90 {
		return fmt.Errorf("source: bounding box %v is off the earth", b)
	}

	return nil
}
