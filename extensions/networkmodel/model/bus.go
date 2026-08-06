package model

import (
	"context"
	"fmt"
	"strings"

	nmid "github.com/xraph/blackstart/extensions/networkmodel/id"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Bus is one bus of a network model, projected out of the canonical blob.
//
// Buses were the only hashed canonical entity with no rows. Branch.FromBus,
// Branch.ToBus and Placement.Bus all name one by string, which meant three
// tables referencing a table that did not exist — and it is why asking "what
// voltage does this branch run at" needed a megabyte blob decode rather than
// a query.
//
// Not the whole canonical bus. The injections, the shunts and the limits stay
// in the blob, because nothing queries them and a projection that duplicates
// the encoding is a second encoder waiting to disagree with the first. This
// row carries what a query needs and stops.
type Bus struct {
	// ID names this bus row. Minted at import, outside the content hash.
	ID id.ID `json:"id"`

	// ModelID is the model it belongs to. Buses are not reconciled across
	// models, for the reason branches are not: a different case is a
	// different network.
	ModelID id.ID `json:"model_id"`

	// Ordinal is the canonical bus identifier — "bus-4" — and is what
	// Branch.FromBus, Branch.ToBus and Placement.Bus hold.
	Ordinal string `json:"ordinal"`

	// VNominalV is the voltage this bus sits at, line-to-line RMS, in VOLTS.
	//
	// Copied unconverted from canonical.Bus.VNominalV. It is the one bus field
	// already SI on both sides, which is why wire.busOf copies it too, and a
	// conversion here would be a second place to get a unit wrong.
	//
	// Zero means the source stated none. Read it through
	// canonical.StatedNominalV rather than comparing it here: that function is
	// the one definition of "states a voltage", and it exists because this
	// question had been answered three different ways.
	VNominalV float64 `json:"v_nominal_v,omitempty"`
}

// Validate reports whether a bus can be stored.
func (b Bus) Validate() error {
	if b.ID.IsZero() {
		return fmt.Errorf("%w: a bus needs an identifier", bserr.ErrInvalid)
	}

	if got := b.ID.Prefix(); got != nmid.Bus {
		return fmt.Errorf(
			"%w: bus identifier %s has prefix %q, not %q",
			bserr.ErrInvalid, b.ID, got, nmid.Bus)
	}

	if b.ModelID.IsZero() {
		return fmt.Errorf("%w: bus %s names no model", bserr.ErrInvalid, b.ID)
	}

	if strings.TrimSpace(b.Ordinal) == "" {
		return fmt.Errorf("%w: bus %s names no ordinal", bserr.ErrInvalid, b.ID)
	}

	// Zero is permitted and negative is not. Zero is how the canonical form
	// spells "this bus states none", and the IEEE cases produce it on every
	// bus. A negative voltage is not an absence — it is a number nothing could
	// have measured, and letting it into a row means a consumer that forgot
	// StatedNominalV converts against it and produces a negative rating.
	if b.VNominalV < 0 {
		return fmt.Errorf(
			"%w: bus %s states %v volts", bserr.ErrInvalid, b.ID, b.VNominalV)
	}

	return nil
}

// BusStore reads and writes the buses of a model.
type BusStore interface {
	// PutAll writes a model's buses, replacing any it already had.
	//
	// All at once rather than one at a time, for BranchStore.PutAll's reason:
	// they arrive together from one import, and a partial set is a model whose
	// ordinals do not cover it — which every lookup downstream reads as "that
	// bus does not exist" rather than as an interrupted write.
	PutAll(ctx context.Context, modelID id.ID, in []Bus) error

	// ForModel returns a model's buses in ordinal order.
	ForModel(ctx context.Context, modelID id.ID) ([]Bus, error)

	// ByOrdinal finds one bus by the name a branch calls it.
	//
	// The lookup a branch makes to find the voltage it runs at. Returns
	// bserr.ErrNotFound when the model has no such ordinal, which means either
	// a broken model or one imported before buses were projected — worth
	// reporting rather than treating as an empty result.
	ByOrdinal(ctx context.Context, modelID id.ID, ordinal string) (Bus, error)
}
