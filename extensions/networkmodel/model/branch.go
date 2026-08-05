package model

import (
	"context"
	"fmt"
	"strings"

	nmid "github.com/xraph/blackstart/extensions/networkmodel/id"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Branch is one branch of a network model, addressable.
//
// The canonical encoding names branches by position — "branch-3" — because
// MATPOWER branches carry no identifier of their own. That string is stable
// for an unchanged file, which is what makes the content hash reproducible,
// and it names nothing outside the blob it came from. This row is what turns
// it into something a route, a conductor assignment or a cascade event can
// refer to.
//
// Stored beside the blob, never inside it. The content hash covers physics —
// what the solver reads — and a hash that changed because the platform
// started recording names would invalidate every result citing it for no
// physical reason.
type Branch struct {
	// ID names this branch.
	ID id.ID `json:"id"`

	// ModelID is the model it belongs to. Branches are not reconciled across
	// models: a different case is a different network, and two branches that
	// happen to connect the same buses are not known to be the same conductor.
	ModelID id.ID `json:"model_id"`

	// Ordinal is the identifier the encoder and the solver use — "branch-3".
	//
	// Stored rather than recomputed from position, because "the third branch
	// in the file" is a fact about one import, and a re-parse that reordered
	// rows would silently repoint every identifier at a different conductor.
	Ordinal string `json:"ordinal"`

	// FromBus and ToBus are the canonical bus identifiers at each end. They
	// are what a placement is looked up by, which is how a branch acquires
	// two points on the earth and therefore a route.
	FromBus string `json:"from_bus"`
	ToBus   string `json:"to_bus"`

	// Alias is what the source called this branch. Kept because the parser
	// already computes it and this platform discards no source identifier.
	Alias string `json:"alias,omitempty"`
}

// Validate reports whether a branch can be stored.
func (b Branch) Validate() error {
	if b.ID.IsZero() {
		return fmt.Errorf("%w: a branch needs an identifier", bserr.ErrInvalid)
	}

	if got := b.ID.Prefix(); got != nmid.Branch {
		return fmt.Errorf(
			"%w: branch identifier %s has prefix %q, not %q",
			bserr.ErrInvalid, b.ID, got, nmid.Branch)
	}

	if b.ModelID.IsZero() {
		return fmt.Errorf(
			"%w: branch %s names no model; a branch outside a network is not one",
			bserr.ErrInvalid, b.ID)
	}

	if strings.TrimSpace(b.Ordinal) == "" {
		return fmt.Errorf(
			"%w: branch %s has no ordinal, so nothing the solver names can find it",
			bserr.ErrInvalid, b.ID)
	}

	if strings.TrimSpace(b.FromBus) == "" || strings.TrimSpace(b.ToBus) == "" {
		return fmt.Errorf(
			"%w: branch %s does not name both its buses", bserr.ErrInvalid, b.ID)
	}

	if b.FromBus == b.ToBus {
		return fmt.Errorf(
			"%w: branch %s runs from bus %s to itself", bserr.ErrInvalid, b.ID, b.FromBus)
	}

	return nil
}

// BranchStore holds the branches of every model.
type BranchStore interface {
	// PutAll writes a model's branches, replacing any it already had.
	//
	// All at once rather than one at a time: they arrive together from one
	// import, and a partial set is a model whose ordinals do not cover it —
	// which every lookup downstream would read as "that branch does not
	// exist" rather than as an interrupted write.
	PutAll(ctx context.Context, modelID id.ID, in []Branch) error

	// ForModel returns a model's branches in ordinal order.
	ForModel(ctx context.Context, modelID id.ID) ([]Branch, error)

	// ByOrdinal finds one branch by the name the solver uses for it.
	//
	// The lookup a cascade event makes. Returns bserr.ErrNotFound when the
	// model has no such ordinal, which means the run and the model disagree
	// — worth reporting rather than treating as an empty result.
	ByOrdinal(ctx context.Context, modelID id.ID, ordinal string) (Branch, error)
}
