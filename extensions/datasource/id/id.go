// Package id declares the datasource extension's identifier prefixes.
//
// Prefixes are registered with the central registry from init, so a collision
// with another extension stops the process at startup with both owners named
// rather than surfacing later as identifiers that parse as the wrong type.
package id

import "github.com/xraph/blackstart/pkg/id"

// Owner is the extension these prefixes belong to.
const Owner = "datasource"

// Identifier prefixes. Immutable once shipped: changing one invalidates every
// stored identifier that uses it, every external alias that resolved to one,
// and every provenance record naming one.
const (
	// Source is a public data source — NOAA HRRR, ORNL EAGLE-I, the Texas A&M
	// synthetic grids.
	Source id.Prefix = "dsrc"

	// Feed is one retrievable resource within a source.
	Feed id.Prefix = "dfeed"
)

//nolint:gochecknoinits // prefix registration is init-time by design
func init() {
	id.MustRegister(Source, Owner, "Source")
	id.MustRegister(Feed, Owner, "Feed")
}

// NewSource mints a source identifier.
func NewSource() id.ID { return id.MustNew(Source) }

// NewFeed mints a feed identifier.
func NewFeed() id.ID { return id.MustNew(Feed) }

// ParseSource reads a source identifier, rejecting any other type.
func ParseSource(s string) (id.ID, error) { return id.ParseWithPrefix(s, Source) }

// ParseFeed reads a feed identifier, rejecting any other type.
func ParseFeed(s string) (id.ID, error) { return id.ParseWithPrefix(s, Feed) }
