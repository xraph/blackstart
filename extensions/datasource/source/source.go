// Package source models a public data source.
//
// The registry these describe is the one in the project README and in spec §8:
// every dataset blackstart consumes must be publicly downloadable without an
// NDA, a license negotiation, or a utility relationship. Recording the sources
// as first-class entities rather than as a table in a document is what lets a
// provenance record point at one, and what lets `bsctl` answer "where did this
// number come from" mechanically.
package source

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/store"
)

// Kind classifies what a source provides. It is a closed set because the
// registry is a closed set: adding a source outside spec §8 is a decision that
// needs discussion, and a free-text field would let one arrive unnoticed.
type Kind string

const (
	// KindNetwork is a power-system network model — the Texas A&M synthetic
	// grids, the IEEE test cases.
	KindNetwork Kind = "network"

	// KindWeather is numerical weather prediction or observation.
	KindWeather Kind = "weather"

	// KindOutage is historical outage and reliability data.
	KindOutage Kind = "outage"

	// KindGeospatial is terrain, land cover, or infrastructure geometry.
	KindGeospatial Kind = "geospatial"

	// KindProtocol is sample or conformance data for a grid protocol.
	KindProtocol Kind = "protocol"
)

// Kinds returns every valid kind, for validation and for the API surface.
func Kinds() []Kind {
	return []Kind{KindNetwork, KindWeather, KindOutage, KindGeospatial, KindProtocol}
}

// Valid reports whether the kind is one of the known set.
func (k Kind) Valid() bool {
	return slices.Contains(Kinds(), k)
}

// Source is a public data source.
//
// The facts here change over time and the changes matter: a source's license
// is amended, its homepage moves, an agency withdraws a dataset. A provenance
// record written last year must still resolve the license that applied then,
// which is why this entity is bitemporal.
type Source struct {
	// ID is the stable identifier.
	ID id.ID `json:"id"`

	// Slug is the short human-readable handle used on the command line:
	// `bsctl import case --source tamu-activsg`.
	Slug string `json:"slug"`

	// Name is the full name as the publisher writes it.
	Name string `json:"name"`

	// Kind classifies what it provides.
	Kind Kind `json:"kind"`

	// Provides is a one-line description, mirroring the README's registry
	// table.
	Provides string `json:"provides"`

	// Homepage is where a human goes to read about it.
	Homepage string `json:"homepage"`

	// License names the terms the data is published under. Free text, because
	// public agency data is published under a wide range of statements and
	// many are not SPDX identifiers — "U.S. Government Work, public domain"
	// is the common one and has no identifier.
	License string `json:"license"`

	// RequiresAttribution records whether use obliges a citation. Several of
	// these do, and NOTICE has to stay correct.
	RequiresAttribution bool `json:"requiresAttribution"`
}

// slugPattern keeps slugs usable as command-line arguments and URL segments.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Validate reports whether the source is well-formed.
func (s Source) Validate() error {
	if s.ID.IsZero() {
		return fmt.Errorf("%w: a source needs an identifier", bserr.ErrInvalid)
	}

	if s.ID.Prefix() != "dsrc" {
		return fmt.Errorf("%w: %s is not a source identifier", bserr.ErrInvalid, s.ID)
	}

	if !slugPattern.MatchString(s.Slug) {
		return fmt.Errorf(
			"%w: slug %q must be lowercase words joined by hyphens; it is used "+
				"as a command-line argument and a URL segment",
			bserr.ErrInvalid, s.Slug)
	}

	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("%w: a source needs a name", bserr.ErrInvalid)
	}

	if !s.Kind.Valid() {
		return fmt.Errorf(
			"%w: %q is not a known source kind; the registry is a closed set, "+
				"and adding a source outside it needs discussion first",
			bserr.ErrInvalid, s.Kind)
	}

	if strings.TrimSpace(s.License) == "" {
		return fmt.Errorf(
			"%w: source %q has no license recorded; the project's public-data "+
				"guarantee depends on knowing the terms of every source",
			bserr.ErrInvalid, s.Slug)
	}

	return nil
}

// Filter narrows a listing.
//
// It is a declarative struct rather than SQL fragments so that it can be
// lowered onto whichever query layer is underneath — today Fabriq's structured
// filter, later a DQL document.
type Filter struct {
	// Kind restricts to one classification. Empty matches all.
	Kind Kind

	// Slug matches one source exactly. Empty matches all.
	Slug string
}

// Store reads and writes sources.
//
// The temporal context travels in ctx, so there is no AsOf variant of any
// method here — see pkg/bitemporal. An implementation that ignores it is a
// bug the Temporal capability interface names.
type Store interface {
	Get(ctx context.Context, id id.ID) (Source, error)
	GetBySlug(ctx context.Context, slug string) (Source, error)
	List(ctx context.Context, f Filter, p store.Page) ([]Source, error)
	Create(ctx context.Context, s Source) error
	Update(ctx context.Context, s Source) error
}

// Sentinel errors. Each wraps a shared kind, so callers can ask either the
// specific question or the general one.
//
//nolint:gochecknoglobals // sentinel errors
var (
	ErrNotFound      = fmt.Errorf("datasource: source not found: %w", bserr.ErrNotFound)
	ErrSlugTaken     = fmt.Errorf("datasource: slug already in use: %w", bserr.ErrConflict)
	ErrAlreadyExists = fmt.Errorf("datasource: source already exists: %w", bserr.ErrConflict)
)
