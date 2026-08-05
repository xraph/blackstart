// Package forecast is what a numerical weather model predicted, and when it
// said so.
//
// Two time axes, kept apart throughout. Issuance is when the model ran; valid
// time is the moment it describes. Collapsing them is the most common source
// of silent leakage in the outage-prediction literature — a model scored
// against a forecast revised after the fact has seen information that did not
// exist when it claims to have predicted — which is why every sample here
// carries both and why neither is derived from the other at read time.
package forecast

import (
	"fmt"
	"strings"
	"time"

	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/weather/field"
	"github.com/xraph/blackstart/extensions/weather/model"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Model names a numerical weather prediction model.
//
// An alias rather than a definition. The field vocabulary needs the model too
// — HRRR and GFS agree on parameter numbers but not always on levels, and
// three fields exist in one and not the other — and this package is built on
// that one, so defining the type here would make the vocabulary import the
// forecast types and close a cycle. It lives in its own package; this alias
// keeps every existing reference working.
type Model = model.Model

// The models, re-exported so callers do not need a second import for a value
// they were already reading from this package.
const (
	ModelHRRR = model.HRRR
	ModelGFS  = model.GFS
)

// Models returns every model in the set.
func Models() []Model { return model.All() }

// CycleStatus is how far a cycle has been processed.
type CycleStatus string

const (
	// StatusPending means the cycle is known and nothing has been fetched.
	StatusPending CycleStatus = "pending"

	// StatusFetched means the wanted fields arrived and were recorded, and
	// none has been sampled onto spans yet.
	StatusFetched CycleStatus = "fetched"

	// StatusSampled means the fields have been sampled.
	StatusSampled CycleStatus = "sampled"

	// StatusFailed means the cycle could not be completed.
	//
	// Kept rather than deleted. A cycle that was tried and failed is a fact
	// about what this system knew, and removing the row would make it
	// indistinguishable from a cycle nobody attempted — which is exactly the
	// distinction a backfill pass needs.
	StatusFailed CycleStatus = "failed"
)

// Valid reports whether this is a status the model knows.
func (s CycleStatus) Valid() bool {
	return s == StatusPending || s == StatusFetched ||
		s == StatusSampled || s == StatusFailed
}

// Cycle is one run of one model.
type Cycle struct {
	// ID identifies this cycle.
	ID id.ID `json:"id"`

	// FeedID names the registry feed it came from, so a sample traces back to
	// a license and a publisher rather than to a bare URL.
	FeedID id.ID `json:"feed_id"`

	// Model is which model ran.
	//
	// Carried alongside FeedID rather than dereferenced from it, because it
	// decides horizon, cadence and grid — and resolving a feed on every such
	// decision would be indirection for its own sake.
	Model Model `json:"model"`

	// IssuedAt is when the model ran: the knowledge-time axis.
	IssuedAt time.Time `json:"issued_at"`

	// Status is how far this cycle has been processed.
	Status CycleStatus `json:"status"`
}

// Validate reports whether a cycle can be stored.
func (c Cycle) Validate() error {
	if c.ID.IsZero() {
		return fmt.Errorf("%w: a cycle needs an identifier", bserr.ErrInvalid)
	}

	if c.FeedID.IsZero() {
		return fmt.Errorf(
			"%w: cycle %s names no feed; a retrieval that cannot say which "+
				"registry entry it came from cannot say what license it carries",
			bserr.ErrInvalid, c.ID)
	}

	if !c.Model.Valid() {
		return fmt.Errorf(
			"%w: %q is not a model this platform knows; the set is closed "+
				"because adding one is a decision about which public data is "+
				"consumed", bserr.ErrInvalid, c.Model)
	}

	if c.IssuedAt.IsZero() {
		return fmt.Errorf(
			"%w: cycle %s has no issuance time; without it a forecast cannot be "+
				"told from an observation, and a backtest cannot tell what was "+
				"knowable when", bserr.ErrInvalid, c.ID)
	}

	if !c.Status.Valid() {
		return fmt.Errorf(
			"%w: %q is not a cycle status", bserr.ErrInvalid, c.Status)
	}

	return nil
}

// Grid is one retrieved GRIB2 message set: one field, at one forecast hour, of
// one cycle.
//
// The unit a dataset version is recorded against, because it is the unit
// actually fetched — pulled by byte range out of a much larger file, so the
// hash covers what was read rather than what was published.
type Grid struct {
	// ID identifies this retrieval.
	ID id.ID `json:"id"`

	// CycleID names the run it belongs to.
	CycleID id.ID `json:"cycle_id"`

	// ForecastHour is hours after issuance. Zero is the analysis.
	ForecastHour int `json:"forecast_hour"`

	// Field is which quantity this holds.
	Field field.Field `json:"field"`

	// BlobRef names the stored bytes in the content-addressable plane.
	BlobRef string `json:"blob_ref"`

	// DatasetVersionID names the provenance record for the retrieval.
	DatasetVersionID id.ID `json:"dataset_version_id"`

	// NX and NY are the grid dimensions and Projection names its shape —
	// "lambert" or "latlon". Recorded so an operator can tell a CONUS grid
	// from a global one without decoding the bytes again.
	NX         int    `json:"nx"`
	NY         int    `json:"ny"`
	Projection string `json:"projection"`

	// SampledAt is when spans were sampled from this grid, zero if never.
	//
	// A timestamp rather than a boolean, because "when" is the question asked
	// when a re-sample is being considered and a boolean would need a second
	// column to answer it.
	//
	// omitzero rather than omitempty: omitempty does nothing on a struct, so
	// an unsampled grid would serialize a zero time that reads as year 1
	// rather than omitting the field.
	//
	// optional:"true" as well, and the tag is not decoration. Forge's schema
	// generator reads `omitempty` and pointer-ness but not `omitzero`, so
	// without it the specification declares sampled_at required while the
	// encoder omits it — and a generated client then types it non-optional
	// and reads undefined on exactly the grids that were never sampled, which
	// are the ones the field exists to identify.
	SampledAt time.Time `json:"sampled_at,omitzero" optional:"true"`

	// Covered and Total are how many of the spans offered to the sampler fell
	// inside this grid's domain.
	//
	// Both, rather than a ratio. HRRR is CONUS-only, so a span outside it
	// yields no sample at all — and absence has to be visible as absence
	// rather than inferred from a count that could equally mean nothing was
	// tried.
	Covered int `json:"covered"`
	Total   int `json:"total"`
}

// Validate reports whether a grid can be stored.
func (g Grid) Validate() error {
	if g.ID.IsZero() || g.CycleID.IsZero() {
		return fmt.Errorf("%w: a grid needs both identifiers", bserr.ErrInvalid)
	}

	if !g.Field.Valid() {
		return fmt.Errorf(
			"%w: grid %s holds %q, which is not a field in the vocabulary",
			bserr.ErrInvalid, g.ID, g.Field)
	}

	if g.ForecastHour < 0 {
		return fmt.Errorf(
			"%w: grid %s has forecast hour %d; a forecast cannot precede the run "+
				"that produced it", bserr.ErrInvalid, g.ID, g.ForecastHour)
	}

	if g.BlobRef == "" {
		return fmt.Errorf(
			"%w: grid %s names no stored bytes; a catalog row without them "+
				"cannot be re-sampled and cannot be checked against its hash",
			bserr.ErrInvalid, g.ID)
	}

	if g.DatasetVersionID.IsZero() {
		return fmt.Errorf(
			"%w: grid %s has no dataset version; a weather field a result was "+
				"conditioned on must be identifiable later, or the result is not "+
				"reproducible", bserr.ErrInvalid, g.ID)
	}

	return nil
}

// ValidAt returns the moment this grid describes, given its cycle's issuance.
//
// Derived rather than stored, because two columns that must agree are two
// columns that eventually will not.
func (g Grid) ValidAt(issuedAt time.Time) time.Time {
	return issuedAt.Add(time.Duration(g.ForecastHour) * time.Hour)
}

// Sample is one field's value at one span, from one issuance.
type Sample struct {
	// SpanID names where. A span rather than a route, because failure is per
	// span: a conductor breaks between two structures, an ice load is local,
	// and a long line crosses several weather systems at once.
	SpanID id.ID `json:"span_id"`

	// Field names what.
	Field field.Field `json:"field"`

	// IssuedAt is when the model said so; ValidAt is the moment it describes.
	IssuedAt time.Time `json:"issued_at"`
	ValidAt  time.Time `json:"valid_at"`

	// Value is the magnitude, in the unit this field's spec names. Stored as
	// the source reported it rather than converted — see the field package.
	Value float64 `json:"value"`
}

// Validate reports whether a sample can be stored.
func (s Sample) Validate() error {
	if s.SpanID.IsZero() {
		return fmt.Errorf("%w: a sample needs a span", bserr.ErrInvalid)
	}

	if !s.Field.Valid() {
		return fmt.Errorf(
			"%w: %q is not a field in the vocabulary", bserr.ErrInvalid, s.Field)
	}

	if s.IssuedAt.IsZero() || s.ValidAt.IsZero() {
		return fmt.Errorf(
			"%w: sample of %q at span %s needs both an issuance and a valid time",
			bserr.ErrInvalid, s.Field, s.SpanID)
	}

	if s.ValidAt.Before(s.IssuedAt) {
		return fmt.Errorf(
			"%w: sample of %q at span %s is valid at %s, before its issuance at "+
				"%s; a forecast cannot describe a moment that preceded the run "+
				"that produced it, so this is a sampling bug rather than a datum",
			bserr.ErrInvalid, s.Field, s.SpanID,
			s.ValidAt.UTC().Format(time.RFC3339),
			s.IssuedAt.UTC().Format(time.RFC3339))
	}

	return nil
}

// IssuanceLayout is how issuance is rendered inside a series key.
//
// RFC 3339 basic format in UTC. Pinned as a constant rather than formatted at
// each call site, because a key written one way and composed another way for
// reading yields an empty series rather than an error — the quietest failure
// this package could have.
const IssuanceLayout = "20060102T1504Z"

// keyDelimiter separates a series key's three parts.
//
// A pipe, which appears in none of them: identifiers are prefix_ULID, field
// names are lowercase letters, digits and underscores, and issuance is digits
// with two letters.
const keyDelimiter = "|"

// keyParts is how many pieces a forecast series key has.
const keyParts = 3

// SampleKey composes the series key a sample is stored under.
//
// The Timeseries port carries one string key and one float per point, with no
// column for issuance — so issuance lives here. That is not a workaround: a
// series key is how a time-series store names a series, and what identifies
// one here is exactly (span, field, issuance).
func SampleKey(spanID id.ID, f field.Field, issuedAt time.Time) string {
	return strings.Join([]string{
		spanID.String(),
		string(f),
		issuedAt.UTC().Format(IssuanceLayout),
	}, keyDelimiter)
}

// ParseSampleKey takes a series key apart.
//
// # Errors
//
// Returns an error wrapping bserr.ErrInvalid when the key has the wrong shape,
// does not begin with a span identifier, names a field outside the vocabulary,
// or carries an issuance that is not in IssuanceLayout.
func ParseSampleKey(key string) (id.ID, field.Field, time.Time, error) {
	piece := strings.Split(key, keyDelimiter)
	if len(piece) != keyParts {
		return id.Nil, "", time.Time{}, fmt.Errorf(
			"%w: series key %q has %d parts, want %d (span, field, issuance)",
			bserr.ErrInvalid, key, len(piece), keyParts)
	}

	spanID, err := gid.ParseSpan(piece[0])
	if err != nil {
		return id.Nil, "", time.Time{}, fmt.Errorf(
			"%w: series key %q does not begin with a span identifier: %w",
			bserr.ErrInvalid, key, err)
	}

	found := field.Field(piece[1])
	if !found.Valid() {
		return id.Nil, "", time.Time{}, fmt.Errorf(
			"%w: series key %q names field %q, which is not in the vocabulary",
			bserr.ErrInvalid, key, piece[1])
	}

	issuedAt, err := time.Parse(IssuanceLayout, piece[2])
	if err != nil {
		return id.Nil, "", time.Time{}, fmt.Errorf(
			"%w: series key %q has issuance %q, which is not in layout %s: %w",
			bserr.ErrInvalid, key, piece[2], IssuanceLayout, err)
	}

	return spanID, found, issuedAt, nil
}
