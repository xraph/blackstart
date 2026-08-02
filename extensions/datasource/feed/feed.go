// Package feed models one retrievable resource within a data source.
//
// A source is the publisher; a feed is a thing you can actually fetch. NOAA is
// one source with many feeds — HRRR, NBM, MRMS — each with its own URL shape,
// format and cadence. Keeping them separate is what lets a dataset version
// point at the exact feed it came from rather than at an agency.
package feed

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/store"
)

// Format is the wire format of a feed's payload.
type Format string

const (
	FormatGRIB2     Format = "grib2"
	FormatCSV       Format = "csv"
	FormatJSON      Format = "json"
	FormatXML       Format = "xml"
	FormatShapefile Format = "shapefile"
	FormatGeoJSON   Format = "geojson"
	FormatMATPOWER  Format = "matpower"
	FormatPSSERaw   Format = "psse-raw"
	FormatSCL       Format = "scl"
	FormatCIMRDF    Format = "cim-rdf"
	FormatZarr      Format = "zarr"
	FormatUnknown   Format = "unknown"
)

// Formats returns every known format.
func Formats() []Format {
	return []Format{
		FormatGRIB2, FormatCSV, FormatJSON, FormatXML, FormatShapefile,
		FormatGeoJSON, FormatMATPOWER, FormatPSSERaw, FormatSCL,
		FormatCIMRDF, FormatZarr, FormatUnknown,
	}
}

// Valid reports whether the format is known.
func (f Format) Valid() bool {
	return slices.Contains(Formats(), f)
}

// Feed is one retrievable resource.
type Feed struct {
	// ID is the stable identifier.
	ID id.ID `json:"id"`

	// SourceID is the publisher. This is the field the graph projection
	// follows, and the reason this extension proves the projection works.
	SourceID id.ID `json:"sourceId"`

	// Slug is unique within its source.
	Slug string `json:"slug"`

	// Name is the human-readable name.
	Name string `json:"name"`

	// URL is where the resource lives. It may contain substitution markers
	// for time-partitioned feeds; resolving those is the ingest worker's job,
	// not this entity's.
	URL string `json:"url"`

	// Format is the wire format.
	Format Format `json:"format"`

	// Cadence describes how often it updates, in words rather than as a
	// duration — "hourly", "as published", "annually". Several of these
	// genuinely have no fixed period, and a duration field would force a lie.
	Cadence string `json:"cadence"`
}

// Validate reports whether the feed is well-formed.
func (f Feed) Validate() error {
	if f.ID.IsZero() || f.ID.Prefix() != "dfeed" {
		return fmt.Errorf("%w: a feed needs a feed identifier", bserr.ErrInvalid)
	}

	if f.SourceID.IsZero() || f.SourceID.Prefix() != "dsrc" {
		return fmt.Errorf(
			"%w: feed %q must belong to a source; the graph projection follows "+
				"this field", bserr.ErrInvalid, f.Slug)
	}

	if strings.TrimSpace(f.Slug) == "" {
		return fmt.Errorf("%w: a feed needs a slug", bserr.ErrInvalid)
	}

	if !f.Format.Valid() {
		return fmt.Errorf("%w: %q is not a known format", bserr.ErrInvalid, f.Format)
	}

	if err := validateURL(f.URL); err != nil {
		return err
	}

	return nil
}

// validateURL rejects anything that is not an absolute http or https URL.
//
// A feed URL is fetched by an unattended worker, so the scheme check is a real
// safety property rather than tidiness: a file:// or ftp:// entry would have
// the ingest worker reading the local filesystem or opening a plaintext
// connection on the strength of a database row.
func validateURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("%w: a feed needs a URL", bserr.ErrInvalid)
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: malformed URL %q: %w", bserr.ErrInvalid, raw, err)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf(
			"%w: feed URL %q uses the %q scheme; only http and https are "+
				"accepted, because an unattended worker fetches these",
			bserr.ErrInvalid, raw, parsed.Scheme)
	}

	if parsed.Host == "" {
		return fmt.Errorf("%w: feed URL %q has no host", bserr.ErrInvalid, raw)
	}

	return nil
}

// Filter narrows a listing.
type Filter struct {
	// SourceID restricts to feeds of one source. Zero matches all.
	SourceID id.ID

	// Format restricts to one wire format. Empty matches all.
	Format Format
}

// Store reads and writes feeds.
type Store interface {
	Get(ctx context.Context, id id.ID) (Feed, error)
	List(ctx context.Context, f Filter, p store.Page) ([]Feed, error)
	Create(ctx context.Context, f Feed) error
	Update(ctx context.Context, f Feed) error
}

// Sentinel errors.
//
//nolint:gochecknoglobals // sentinel errors
var (
	ErrNotFound  = fmt.Errorf("datasource: feed not found: %w", bserr.ErrNotFound)
	ErrSlugTaken = fmt.Errorf("datasource: feed slug already in use within this source: %w", bserr.ErrConflict)
)
