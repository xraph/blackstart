// Package id declares the network-model extension's identifier prefixes.
package id

import "github.com/xraph/blackstart/pkg/id"

// Owner is the extension these prefixes belong to.
const Owner = "networkmodel"

// Identifier prefixes. Immutable once shipped.
const (
	// Model is one immutable snapshot of a network, named by content hash.
	Model id.Prefix = "nmdl"

	// Blob is the canonical encoding of a model. A separate entity because it
	// is megabytes where the record is bytes, and one row would drag the
	// encoding through every listing.
	Blob id.Prefix = "nmblob"

	// Branch is one branch of a network model, given identity so that a
	// cascade event naming "branch-3" resolves to something with geometry.
	//
	// Beside the canonical blob and never inside it: the content hash covers
	// physics, and a hash that moved because the platform started recording
	// names would invalidate every result citing it for no physical reason.
	Branch id.Prefix = "br"

	// Placement is where one bus sits on the earth.
	Placement id.Prefix = "nmplc"

	// Bus is one bus of a network model, given identity so that "what voltage
	// does this bus sit at" is a query rather than a blob decode.
	//
	// Beside the canonical blob and never inside it, exactly as Branch is: the
	// content hash covers physics, and identity minted at import is not
	// physics. What the row carries from inside the hash — the ordinal and the
	// nominal voltage — is a projection of it, not a second copy of it.
	Bus id.Prefix = "nmbus"
)

//nolint:gochecknoinits // prefix registration is init-time by design
func init() {
	id.MustRegister(Model, Owner, "NetworkModel")
	id.MustRegister(Blob, Owner, "NetworkModelBlob")
	id.MustRegister(Branch, Owner, "NetworkModelBranch")
	id.MustRegister(Placement, Owner, "NetworkModelPlacement")
	id.MustRegister(Bus, Owner, "NetworkModelBus")
}

// NewModel mints a network-model identifier.
func NewModel() id.ID { return id.MustNew(Model) }

// NewBlob mints a blob identifier.
func NewBlob() id.ID { return id.MustNew(Blob) }

// NewBranch mints a branch identifier.
func NewBranch() id.ID { return id.MustNew(Branch) }

// NewPlacement mints a placement identifier.
func NewPlacement() id.ID { return id.MustNew(Placement) }

// NewBus mints a bus identifier.
func NewBus() id.ID { return id.MustNew(Bus) }

// ParseModel reads a network-model identifier, rejecting any other type.
func ParseModel(s string) (id.ID, error) { return id.ParseWithPrefix(s, Model) }
