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
	"time"

	"github.com/xraph/blackstart/extensions/weather/field"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Model names a numerical weather prediction model.
//
// A closed set, like the source registry it draws from. Adding a model is a
// decision about which public data this platform consumes — spec §8 makes it
// one that needs approval — and a free-text field would let one arrive
// unnoticed.
type Model string

const (
	// ModelHRRR is NOAA's High-Resolution Rapid Refresh: 3 km, CONUS only,
	// issued hourly. The CONUS-only part matters downstream: a span outside
	// its domain has no sample rather than a zero.
	ModelHRRR Model = "hrrr"

	// ModelGFS is NOAA's Global Forecast System: coarser, global, issued four
	// times daily. The fallback where HRRR does not reach, and the longer
	// horizon where it does.
	ModelGFS Model = "gfs"
)

// Models returns every model in the set.
func Models() []Model { return []Model{ModelHRRR, ModelGFS} }

// Valid reports whether this is a model the platform knows.
func (m Model) Valid() bool { return m == ModelHRRR || m == ModelGFS }

// String renders the model name.
func (m Model) String() string { return string(m) }

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
	SampledAt time.Time `json:"sampled_at,omitzero"`

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
