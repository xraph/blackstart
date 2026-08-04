package weather

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	dsfeed "github.com/xraph/blackstart/extensions/datasource/feed"
	"github.com/xraph/blackstart/extensions/provenance/record"
	"github.com/xraph/blackstart/extensions/weather/fetch"
	"github.com/xraph/blackstart/extensions/weather/field"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	wid "github.com/xraph/blackstart/extensions/weather/id"
	"github.com/xraph/blackstart/extensions/weather/model"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// ParserVersion is what read the bytes.
//
// Recorded on every DatasetVersion, because a dataset is only as reproducible
// as the code that interpreted it: a decoder fix changes what the same bytes
// mean, and a result conditioned on the old reading is not reproducible from
// the new one without knowing which was used.
const ParserVersion = "bs-grib/0.1.0"

// Feeds is the part of the datasource registry ingest needs.
//
// An interface declared here rather than the concrete engine, so that this
// extension does not gain the ability to write the registry — and so its tests
// do not need datasource's storage compiled in. The same reasoning provenance
// applies to Recorder.
type Feeds interface {
	GetSourceBySlug(ctx context.Context, slug string) (sourceID id.ID, err error)
	FeedBySlug(ctx context.Context, sourceSlug, feedSlug string) (dsfeed.Feed, error)
}

// Fetcher retrieves products. Satisfied by *fetch.Client.
type Fetcher interface {
	Index(ctx context.Context, url string) (fetch.Records, error)
	Range(ctx context.Context, url string, offset, length int64) ([]byte, error)
}

// IngestRequest asks for one cycle's fields to be retrieved.
type IngestRequest struct {
	// SourceSlug and FeedSlug name the registry entry to fetch from.
	//
	// Slugs, never a URL. Resolving through the registry is the only route to
	// a fetch, so a source outside spec §8 cannot be ingested without first
	// being added to it — which is a decision with a person attached. This is
	// where rule 2.2 stops being a convention someone remembers.
	SourceSlug string
	FeedSlug   string

	// Model is which model's product this is.
	Model model.Model

	// IssuedAt is the cycle to fetch.
	IssuedAt time.Time

	// Hours are the forecast hours wanted. Empty means the analysis alone.
	Hours []int

	// Fields are the fields wanted. Empty means every field the vocabulary
	// knows that this model publishes.
	Fields []field.Field
}

// IngestResult is what one pass did.
type IngestResult struct {
	// CycleID names the run, whether it was created here or already existed.
	CycleID id.ID

	// Retrieved counts field-hours fetched and recorded for the first time.
	Retrieved int

	// Skipped counts field-hours whose bytes were already known, by content
	// hash. A repeat ingest is almost entirely skips, which is what makes
	// bounded backfill cheap to run on every restart.
	Skipped int

	// Absent counts field-hours the publisher has not posted yet. Not a
	// failure: the most recent hour is routinely absent.
	Absent int

	// Failed counts field-hours that errored for any other reason. The pass
	// continues past them — one bad hour should not cost the other seventeen.
	Failed int
}

// Ingest retrieves a cycle's fields and records them.
//
// Retrieval only. Sampling is a separate operation because the two fail
// differently and recover differently: a retrieval is irreplaceable once
// NOAA's rolling archive passes it, and sampling is recomputable from the
// stored bytes forever.
//
// # Errors
//
// Returns an error when the feed cannot be resolved or the cycle cannot be
// recorded — the conditions under which no field-hour could succeed. A failure
// on an individual field-hour is counted, not returned.
func (e *Engine) Ingest(ctx context.Context, in IngestRequest) (IngestResult, error) {
	if e.feeds == nil || e.fetcher == nil {
		return IngestResult{}, errors.New(
			"weather: ingest needs a registry and a fetcher; this engine was built " +
				"for reads only")
	}

	found, err := e.feeds.FeedBySlug(ctx, in.SourceSlug, in.FeedSlug)
	if err != nil {
		return IngestResult{}, fmt.Errorf(
			"weather: resolving feed %s/%s: %w", in.SourceSlug, in.FeedSlug, err)
	}

	sourceID, err := e.feeds.GetSourceBySlug(ctx, in.SourceSlug)
	if err != nil {
		return IngestResult{}, fmt.Errorf(
			"weather: resolving source %s: %w", in.SourceSlug, err)
	}

	cycle, err := e.RecordCycle(ctx, forecast.Cycle{
		ID:       wid.NewCycle(),
		FeedID:   found.ID,
		Model:    in.Model,
		IssuedAt: in.IssuedAt.UTC(),
		Status:   forecast.StatusPending,
	})
	if err != nil {
		return IngestResult{}, err
	}

	result := IngestResult{CycleID: cycle.ID}
	hours := in.Hours

	if len(hours) == 0 {
		hours = []int{0}
	}

	for _, hour := range hours {
		e.ingestHour(ctx, in, found, sourceID, cycle, hour, &result)
	}

	// Fetched rather than sampled: this pass stored bytes and recorded their
	// provenance, and nothing has been sampled onto a span yet.
	if result.Retrieved > 0 || result.Skipped > 0 {
		if err := e.store.Cycles().SetStatus(
			ctx, cycle.ID, forecast.StatusFetched,
		); err != nil {
			return result, err
		}
	}

	return result, nil
}

// ingestHour retrieves every wanted field at one forecast hour.
//
// Counts rather than returns, because one hour failing should not cost the
// others — a 404 on hour 13 of an 18-hour cycle is a gap, not a reason to
// discard twelve successful retrievals.
func (e *Engine) ingestHour(
	ctx context.Context,
	in IngestRequest,
	found dsfeed.Feed,
	sourceID id.ID,
	cycle forecast.Cycle,
	hour int,
	result *IngestResult,
) {
	productURL, err := fetch.ProductURL(found.URL, in.Model, in.IssuedAt, hour)
	if err != nil {
		result.Failed++

		return
	}

	records, err := e.fetcher.Index(ctx, fetch.IndexURL(productURL))
	if err != nil {
		// A cycle the publisher has not posted yet is the ordinary state of
		// the most recent hour, not a failure.
		if errors.Is(err, fetch.ErrNotPublished) {
			result.Absent++

			return
		}

		result.Failed++

		return
	}

	for _, want := range e.fieldsFor(in) {
		spec, err := field.SpecFor(want, in.Model)
		if err != nil {
			// A field this model does not publish is expected — three of them
			// exist in HRRR and not GFS — so it is neither retrieved nor failed.
			if errors.Is(err, field.ErrNotPublished) {
				continue
			}

			result.Failed++

			continue
		}

		switch e.ingestField(ctx, productURL, records, spec, sourceID, cycle, hour) {
		case outcomeRetrieved:
			result.Retrieved++
		case outcomeSkipped:
			result.Skipped++
		case outcomeFailed:
			result.Failed++
		}
	}
}

// outcome is what one field-hour did.
type outcome int

const (
	outcomeRetrieved outcome = iota
	outcomeSkipped
	outcomeFailed
)

// ingestField retrieves one field at one hour.
func (e *Engine) ingestField(
	ctx context.Context,
	productURL string,
	records fetch.Records,
	spec field.Spec,
	sourceID id.ID,
	cycle forecast.Cycle,
	hour int,
) outcome {
	offset, length, ok := records.Locate(spec.IndexKey())
	if !ok {
		// The product does not carry this field at this hour. Common for
		// accumulations at hour zero, which have nothing to accumulate over.
		return outcomeSkipped
	}

	body, err := e.fetcher.Range(ctx, productURL, offset, length)
	if err != nil {
		return outcomeFailed
	}

	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])

	// The provenance record comes before the bytes are stored, and its dedupe
	// by content hash is what makes a repeat ingest cheap: identical bytes are
	// one version however many times they are fetched, so nothing derived from
	// the first retrieval is invalidated by the second.
	dataset, err := e.recorder.RecordDataset(ctx, record.DatasetVersion{
		SourceID:      sourceID,
		URL:           productURL,
		ContentHash:   hash,
		SizeBytes:     int64(len(body)),
		ParserVersion: ParserVersion,
		RetrievedAt:   e.now().UTC(),
	})
	if err != nil {
		return outcomeFailed
	}

	// A grid already recorded for this field-hour means these bytes are known.
	// The unique index would refuse the write anyway; asking first turns the
	// common case of a repeat pass from an error into a count.
	if existing, err := e.store.Grids().ForCycle(ctx, cycle.ID); err == nil {
		for _, grid := range existing {
			if grid.Field == spec.Field && grid.ForecastHour == hour {
				return outcomeSkipped
			}
		}
	}

	reference, err := e.store.Blobs().Put(ctx, body)
	if err != nil {
		return outcomeFailed
	}

	err = e.store.Grids().Create(ctx, forecast.Grid{
		ID:               wid.NewGrid(),
		CycleID:          cycle.ID,
		ForecastHour:     hour,
		Field:            spec.Field,
		BlobRef:          reference,
		DatasetVersionID: dataset.ID,
	})
	if err != nil {
		// A conflict means another pass won the race, which is a skip rather
		// than a failure: the field-hour is recorded either way.
		if errors.Is(err, bserr.ErrConflict) {
			return outcomeSkipped
		}

		return outcomeFailed
	}

	return outcomeRetrieved
}

// fieldsFor is the field list a request asks for.
func (e *Engine) fieldsFor(in IngestRequest) []field.Field {
	if len(in.Fields) > 0 {
		return in.Fields
	}

	return field.Fields()
}
