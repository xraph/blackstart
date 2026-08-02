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
)

//nolint:gochecknoinits // prefix registration is init-time by design
func init() {
	id.MustRegister(Model, Owner, "NetworkModel")
	id.MustRegister(Blob, Owner, "NetworkModelBlob")
}

// NewModel mints a network-model identifier.
func NewModel() id.ID { return id.MustNew(Model) }

// NewBlob mints a blob identifier.
func NewBlob() id.ID { return id.MustNew(Blob) }

// ParseModel reads a network-model identifier, rejecting any other type.
func ParseModel(s string) (id.ID, error) { return id.ParseWithPrefix(s, Model) }
