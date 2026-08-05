package model

import (
	"context"
	"fmt"
	"math"
	"strings"

	nmid "github.com/xraph/blackstart/extensions/networkmodel/id"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Placement is where one bus sits on the earth.
//
// Persisted rather than passed. Coordinates arrive in a CSV beside a case —
// MATPOWER has no field for them, which is also why they stay out of the
// content hash: a substation moving fifty meters must not invalidate every
// result computed for the network. Storing them means a branch has endpoints
// to be traced between, which is what a route, and therefore a blast radius,
// needs.
//
// A model with no placements is a legitimate state. The IEEE cases carry no
// coordinates and never will, and everything downstream reports their absence
// as a count rather than an error.
type Placement struct {
	// ID names this placement.
	ID id.ID `json:"id"`

	// ModelID is the model whose bus this is.
	ModelID id.ID `json:"model_id"`

	// Bus is the canonical bus identifier, matching Branch.FromBus and ToBus.
	Bus string `json:"bus"`

	// Lon and Lat are degrees. Elevation is meters above the ellipsoid, zero
	// when the source did not say — a bus at sea level and a bus whose height
	// nobody recorded are not distinguished, and neither the renderer nor the
	// geodetic conversion needs them to be.
	Lon       float64 `json:"lon"`
	Lat       float64 `json:"lat"`
	Elevation float64 `json:"elevation,omitempty"`
}

// Validate reports whether a placement can be stored.
func (p Placement) Validate() error {
	if p.ID.IsZero() {
		return fmt.Errorf("%w: a placement needs an identifier", bserr.ErrInvalid)
	}

	if got := p.ID.Prefix(); got != nmid.Placement {
		return fmt.Errorf(
			"%w: placement identifier %s has prefix %q, not %q",
			bserr.ErrInvalid, p.ID, got, nmid.Placement)
	}

	if p.ModelID.IsZero() {
		return fmt.Errorf(
			"%w: placement %s names no model", bserr.ErrInvalid, p.ID)
	}

	if strings.TrimSpace(p.Bus) == "" {
		return fmt.Errorf(
			"%w: placement %s names no bus", bserr.ErrInvalid, p.ID)
	}

	// Checked here rather than at the renderer. A longitude of 200 draws
	// somewhere, so the map reads as a rendering fault when it is a data one.
	if math.Abs(p.Lon) > 180 {
		return fmt.Errorf(
			"%w: placement %s has longitude %v", bserr.ErrInvalid, p.ID, p.Lon)
	}

	if math.Abs(p.Lat) > 90 {
		return fmt.Errorf(
			"%w: placement %s has latitude %v", bserr.ErrInvalid, p.ID, p.Lat)
	}

	return nil
}

// PlacementStore holds where each bus is.
type PlacementStore interface {
	// PutAll writes a model's placements, replacing any it already had.
	//
	// Replacing rather than merging: a coordinate file is the operator's
	// statement of where the network is, and a merge would leave a bus at a
	// position the file no longer claims with nothing saying so.
	PutAll(ctx context.Context, modelID id.ID, in []Placement) error

	// ForModel returns every placement a model has.
	ForModel(ctx context.Context, modelID id.ID) ([]Placement, error)
}
