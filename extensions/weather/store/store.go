// Package store composes the weather extension's per-domain stores.
//
// Four, split along the line that matters here: three relational stores for
// the catalog — cycles, grids and stations — and one sample store for the
// numbers. The catalog is small, queried by predicate, and carries the
// provenance links. The samples are bulk, queried by key and time window, and
// live in a Timescale hypertable that fabriq's Timeseries port writes.
//
// None of these entities is bitemporal, which is a departure from the "by
// default" of spec §2.1 and a deliberate one. A weather forecast's two time
// axes are its own data — issuance and valid time, both on every sample — not
// a property of the row that holds it. Cycles and grids are not corrected
// after the fact either; a cycle advances through statuses and a grid is
// written once. Layering row versioning over that would record the same two
// axes twice and let them disagree.
package store

import (
	"context"
	"time"

	"github.com/xraph/blackstart/extensions/weather/forecast"
	"github.com/xraph/blackstart/extensions/weather/observation"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/store"
)

// Store is everything the weather extension persists.
type Store interface {
	store.Lifecycle

	// Cycles reads and writes model runs.
	Cycles() CycleStore

	// Grids reads and writes retrieved GRIB2 message sets.
	Grids() GridStore

	// Stations reads and writes surface observing sites.
	Stations() StationStore

	// Samples reads and writes the numbers.
	Samples() SampleStore

	// Blobs holds the GRIB2 bytes each grid was decoded from.
	Blobs() BlobStore
}

// BlobStore holds retrieved GRIB2 messages.
//
// Separate from the Grid row because they have different shapes and different
// lifetimes: the row is small and listed constantly, the message is megabytes
// and read only when a grid is sampled or re-sampled. Keeping the bytes on the
// row would mean every catalog listing drags them along.
//
// Keeping them at all is what makes sampling recomputable. NOAA's rolling
// archive passes, and after it does these bytes are the only copy of what the
// model said — so a sampling bug found next month is fixable, where a
// re-fetch would not be.
type BlobStore interface {
	// Put stores the bytes and returns the reference to read them back by.
	Put(ctx context.Context, body []byte) (string, error)

	// Get reads the bytes a reference names.
	Get(ctx context.Context, reference string) ([]byte, error)
}

// CycleFilter narrows a cycle listing.
//
// A declarative struct rather than SQL fragments, so it can be lowered onto
// whichever query layer is underneath.
type CycleFilter struct {
	// Model restricts to one model. Empty matches all.
	Model forecast.Model

	// Status restricts to one processing stage. Empty matches all.
	Status forecast.CycleStatus
}

// CycleStore holds model runs.
type CycleStore interface {
	// Create writes a new cycle.
	Create(ctx context.Context, in forecast.Cycle) error

	// Get reads one cycle.
	Get(ctx context.Context, want id.ID) (forecast.Cycle, error)

	// ForIssuance finds the cycle of a model at an issuance.
	//
	// The idempotency key of the whole ingest pipeline. Backfill runs on every
	// worker restart, so "have we already got this cycle" is asked far more
	// often than anything else here, and asking it by (model, issuance) rather
	// than by identifier is what lets the caller ask before minting one.
	ForIssuance(
		ctx context.Context, model forecast.Model, issuedAt time.Time,
	) (forecast.Cycle, error)

	// SetStatus advances a cycle through its processing stages.
	SetStatus(ctx context.Context, want id.ID, status forecast.CycleStatus) error

	// List returns cycles, newest issuance first.
	List(ctx context.Context, f CycleFilter, p store.Page) ([]forecast.Cycle, error)
}

// GridStore holds retrieved GRIB2 message sets.
type GridStore interface {
	// Create writes a new grid.
	Create(ctx context.Context, in forecast.Grid) error

	// Get reads one grid.
	Get(ctx context.Context, want id.ID) (forecast.Grid, error)

	// ForCycle reads every grid of a cycle, ordered by forecast hour then
	// field.
	//
	// Read as a whole cycle rather than one at a time, because the question
	// asked of this table is almost always "what did this run give us", and
	// a single grid in isolation says nothing about the run it belongs to.
	ForCycle(ctx context.Context, cycle id.ID) ([]forecast.Grid, error)

	// MarkSampled records that spans were sampled from a grid, and how
	// completely.
	//
	// Covered and total together, not a ratio: HRRR is CONUS-only, so a span
	// outside its domain yields no sample at all, and the two counts are what
	// make that visible as absence rather than as a suspiciously round number.
	MarkSampled(
		ctx context.Context, want id.ID, at time.Time, covered, total int,
	) error
}

// StationStore holds surface observing sites.
type StationStore interface {
	// Put writes a station, replacing any existing one with the same
	// identifier.
	//
	// Replace rather than create, because the station list is re-seeded from
	// the publisher and a re-seed that collided on every unchanged row would
	// be indistinguishable from a failure.
	Put(ctx context.Context, in observation.Station) error

	// Get reads one station.
	Get(ctx context.Context, want id.ID) (observation.Station, error)

	// ByCode finds a station by the publisher's own identifier.
	//
	// The lookup an observation feed needs: readings arrive naming "KAUS", not
	// naming a platform identifier.
	ByCode(ctx context.Context, code string) (observation.Station, error)

	// List returns stations.
	List(ctx context.Context, p store.Page) ([]observation.Station, error)
}

// SampleStore holds the numbers.
//
// Deliberately narrow, and deliberately keyed rather than filtered. The
// backing table is a Timescale hypertable, which sits outside row-level
// security — Timescale's columnstore forbids it — and outside fabriq's
// raw-SQL guard, whose table list is a hardcoded constant this extension
// cannot add to. Tenant isolation there is therefore this interface's
// implementations' own discipline, and keeping the surface this small is what
// makes that reviewable.
type SampleStore interface {
	// WriteForecast writes span samples in bulk.
	//
	// Through fabriq's Timeseries port, which bypasses the outbox by design:
	// per-row events would melt it, so bulk telemetry skips it and the relay
	// publishes conflated deltas instead. "All entity writes go through
	// fabriq" holds; "all data writes" does not.
	WriteForecast(ctx context.Context, samples []forecast.Sample) error

	// ReadForecast reads span samples for the given series keys, over the
	// half-open window [from, to).
	//
	// Keys rather than a span-and-field filter, because the key is the series
	// identity — see forecast.SampleKey. A caller composes the keys it wants
	// from the catalog and asks once, rather than issuing one round trip per
	// series.
	ReadForecast(
		ctx context.Context, keys []string, from, to time.Time,
	) ([]forecast.Sample, error)

	// WriteObservations writes station readings in bulk.
	WriteObservations(ctx context.Context, obs []observation.Observation) error

	// ReadObservations reads station readings for the given series keys, over
	// the half-open window [from, to).
	ReadObservations(
		ctx context.Context, keys []string, from, to time.Time,
	) ([]observation.Observation, error)
}
