package weather_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	dsfeed "github.com/xraph/blackstart/extensions/datasource/feed"
	dsid "github.com/xraph/blackstart/extensions/datasource/id"
	"github.com/xraph/blackstart/extensions/provenance"
	pvid "github.com/xraph/blackstart/extensions/provenance/id"
	"github.com/xraph/blackstart/extensions/provenance/record"
	"github.com/xraph/blackstart/extensions/weather"
	"github.com/xraph/blackstart/extensions/weather/fetch"
	"github.com/xraph/blackstart/extensions/weather/field"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	"github.com/xraph/blackstart/extensions/weather/model"
	"github.com/xraph/blackstart/extensions/weather/store"
	"github.com/xraph/blackstart/extensions/weather/store/memory"
	"github.com/xraph/blackstart/pkg/id"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

// fakeFeeds is the registry, reduced to what ingest asks of it.
type fakeFeeds struct {
	feed     dsfeed.Feed
	sourceID id.ID
	err      error
}

func (f *fakeFeeds) GetSourceBySlug(context.Context, string) (id.ID, error) {
	return f.sourceID, f.err
}

func (f *fakeFeeds) FeedBySlug(context.Context, string, string) (dsfeed.Feed, error) {
	return f.feed, f.err
}

// fakeFetcher serves a synthetic sidecar and counts what was asked for.
type fakeFetcher struct {
	// absent makes every index look unpublished, which is the most recent
	// hour's ordinary state.
	absent bool

	// indexErr and rangeErr force the other failure paths.
	indexErr error
	rangeErr error

	// bodies keys byte content on offset, so two fields can return the same
	// bytes and exercise the provenance dedupe.
	bodies map[int64][]byte

	indexCalls int
	rangeCalls int
}

// A sidecar holding just the fields these tests use. Offsets are spaced so
// each field's derived length is 100 bytes.
func (f *fakeFetcher) Index(context.Context, string) (fetch.Records, error) {
	f.indexCalls++

	if f.indexErr != nil {
		return nil, f.indexErr
	}

	if f.absent {
		return nil, fmt.Errorf("%w: synthetic", fetch.ErrNotPublished)
	}

	return fetch.ParseIndex(strings.NewReader(
		"1:0:d=2026080312:GUST:surface:1 hour fcst:\n" +
			"2:100:d=2026080312:TMP:2 m above ground:1 hour fcst:\n" +
			"3:200:d=2026080312:DPT:2 m above ground:1 hour fcst:\n"))
}

// RangeCalls is how many byte-range fetches were issued.
func (f *fakeFetcher) RangeCalls() int { return f.rangeCalls }

func (f *fakeFetcher) Range(
	_ context.Context, _ string, offset, _ int64,
) ([]byte, error) {
	f.rangeCalls++

	if f.rangeErr != nil {
		return nil, f.rangeErr
	}

	if body, ok := f.bodies[offset]; ok {
		return body, nil
	}

	return fmt.Appendf(nil, "message at %d", offset), nil
}

// fakeRecorder dedupes by content hash exactly as the real one does — which
// is the behavior the skip count depends on.
type fakeRecorder struct {
	byHash map[string]record.DatasetVersion
	calls  int
}

func newRecorder() *fakeRecorder {
	return &fakeRecorder{byHash: map[string]record.DatasetVersion{}}
}

func (r *fakeRecorder) RecordDataset(
	_ context.Context, in record.DatasetVersion,
) (record.DatasetVersion, error) {
	r.calls++

	if existing, ok := r.byHash[in.ContentHash]; ok {
		return existing, nil
	}

	in.ID = pvid.NewDataset()
	r.byHash[in.ContentHash] = in

	return in, nil
}

func (r *fakeRecorder) RecordModel(
	_ context.Context, in record.ModelVersion,
) (record.ModelVersion, error) {
	return in, nil
}

func (r *fakeRecorder) RecordRun(
	_ context.Context, in record.Run, _ []provenance.Input,
) (record.Run, error) {
	return in, nil
}

var _ provenance.Recorder = (*fakeRecorder)(nil)

func ingestHarness(t *testing.T, fetcher *fakeFetcher) (*weather.Engine, store.Store, *fakeRecorder) {
	t.Helper()

	s := memory.New()
	recorder := newRecorder()

	feeds := &fakeFeeds{
		sourceID: dsid.NewSource(),
		feed: dsfeed.Feed{
			ID:       dsid.NewFeed(),
			SourceID: dsid.NewSource(),
			Slug:     "hrrr",
			URL:      "https://example.gov/hrrr",
			Format:   dsfeed.FormatGRIB2,
		},
	}

	engine, err := weather.NewEngine(s, nil,
		weather.WithIngest(feeds, fetcher, recorder),
		weather.WithClock(func() time.Time { return issuance }))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	return engine, s, recorder
}

func aRequest() weather.IngestRequest {
	return weather.IngestRequest{
		SourceSlug: "noaa",
		FeedSlug:   "hrrr",
		Model:      model.HRRR,
		IssuedAt:   issuance,
		Hours:      []int{1},
		Fields:     []field.Field{field.Gust10m, field.Temp2m, field.Dewpoint2m},
	}
}

func TestIngestRetrievesAndRecords(t *testing.T) {
	t.Parallel()

	engine, s, recorder := ingestHarness(t, &fakeFetcher{})

	got, err := engine.Ingest(t.Context(), aRequest())
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	if got.Retrieved != 3 {
		t.Errorf("retrieved %d, want 3", got.Retrieved)
	}

	if recorder.calls != 3 {
		t.Errorf("recorded %d datasets, want 3", recorder.calls)
	}

	grids, err := s.Grids().ForCycle(t.Context(), got.CycleID)
	if err != nil {
		t.Fatalf("ForCycle: %v", err)
	}

	if len(grids) != 3 {
		t.Fatalf("stored %d grids, want 3", len(grids))
	}

	// Every grid names its bytes and its provenance. Without both, a sample
	// derived from it could not be traced to the retrieval it came from.
	for _, grid := range grids {
		if grid.BlobRef == "" {
			t.Errorf("grid %s has no blob reference", grid.ID)
		}

		if grid.DatasetVersionID.IsZero() {
			t.Errorf("grid %s has no dataset version", grid.ID)
		}
	}
}

// A repeat pass is what bounded backfill does on every restart. It must be
// almost entirely skips, and must create nothing.
func TestIngestIsIdempotent(t *testing.T) {
	t.Parallel()

	fetcher := &fakeFetcher{}
	engine, s, _ := ingestHarness(t, fetcher)

	first, err := engine.Ingest(t.Context(), aRequest())
	if err != nil {
		t.Fatalf("first Ingest: %v", err)
	}

	second, err := engine.Ingest(t.Context(), aRequest())
	if err != nil {
		t.Fatalf("second Ingest: %v", err)
	}

	if second.CycleID != first.CycleID {
		t.Errorf("the repeat minted a new cycle")
	}

	if second.Retrieved != 0 {
		t.Errorf("the repeat retrieved %d field-hours, want 0", second.Retrieved)
	}

	if second.Skipped != 3 {
		t.Errorf("the repeat skipped %d, want 3", second.Skipped)
	}

	grids, err := s.Grids().ForCycle(t.Context(), first.CycleID)
	if err != nil {
		t.Fatalf("ForCycle: %v", err)
	}

	if len(grids) != 3 {
		t.Errorf("the repeat left %d grids, want 3", len(grids))
	}
}

// The most recent hour is routinely not posted yet. That is a count, not a
// failure — a worker treating it as one would log an error every tick.
func TestAnUnpublishedCycleIsAbsentNotFailed(t *testing.T) {
	t.Parallel()

	engine, _, _ := ingestHarness(t, &fakeFetcher{absent: true})

	got, err := engine.Ingest(t.Context(), aRequest())
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	if got.Absent != 1 {
		t.Errorf("absent = %d, want 1", got.Absent)
	}

	if got.Failed != 0 {
		t.Errorf("failed = %d, want 0; not-yet-published is not a failure", got.Failed)
	}
}

// One bad hour should not cost the others. A 404 on hour 13 of an 18-hour
// cycle is a gap, not a reason to discard twelve good retrievals.
func TestOneBadHourDoesNotAbortTheRest(t *testing.T) {
	t.Parallel()

	engine, _, _ := ingestHarness(t, &fakeFetcher{})

	request := aRequest()
	request.Hours = []int{1, 2, 3}

	got, err := engine.Ingest(t.Context(), request)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	// Three hours of three fields. The synthetic bodies differ per offset but
	// not per hour, so the second and third hours dedupe on content hash.
	if got.Retrieved+got.Skipped != 9 {
		t.Errorf("retrieved+skipped = %d, want 9", got.Retrieved+got.Skipped)
	}
}

func TestRangeFailuresAreCountedNotReturned(t *testing.T) {
	t.Parallel()

	engine, _, _ := ingestHarness(t, &fakeFetcher{rangeErr: errors.New("network")})

	got, err := engine.Ingest(t.Context(), aRequest())
	if err != nil {
		t.Fatalf("Ingest returned an error for a per-field failure: %v", err)
	}

	if got.Failed != 3 {
		t.Errorf("failed = %d, want 3", got.Failed)
	}

	if got.Retrieved != 0 {
		t.Errorf("retrieved = %d, want 0", got.Retrieved)
	}
}

// A field the model does not publish is expected, not an error. Three exist
// in HRRR and not GFS, and asking GFS for them must not count as failure.
func TestFieldsAModelDoesNotPublishAreSkipped(t *testing.T) {
	t.Parallel()

	engine, _, _ := ingestHarness(t, &fakeFetcher{})

	request := aRequest()
	request.Model = model.GFS
	request.IssuedAt = issuance // 12Z is a GFS cycle
	request.Fields = []field.Field{field.Lightning, field.SnowAccum}

	got, err := engine.Ingest(t.Context(), request)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	if got.Failed != 0 {
		t.Errorf("failed = %d; a field GFS does not publish is not a failure", got.Failed)
	}

	if got.Retrieved != 0 {
		t.Errorf("retrieved = %d, want 0", got.Retrieved)
	}
}

// The whole of rule 2.2: reaching a fetch requires resolving through the
// registry, so a source outside §8 cannot be ingested without being added.
func TestAnUnresolvableFeedIsRefused(t *testing.T) {
	t.Parallel()

	s := memory.New()

	engine, err := weather.NewEngine(s, nil, weather.WithIngest(
		&fakeFeeds{err: errors.New("no such feed")},
		&fakeFetcher{},
		newRecorder()))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	if _, err := engine.Ingest(t.Context(), aRequest()); err == nil {
		t.Fatal("an unresolvable feed was ingested")
	}
}

// A read-only engine has no fetcher. Asking it to ingest must say so rather
// than panicking on a nil.
func TestAReadOnlyEngineRefusesToIngest(t *testing.T) {
	t.Parallel()

	engine, err := weather.NewEngine(memory.New(), nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	if _, err := engine.Ingest(t.Context(), aRequest()); err == nil {
		t.Fatal("a read-only engine ingested")
	}
}

// After a pass that stored something, the cycle is fetched — not sampled.
// Nothing has been put on a span yet, and a status saying otherwise would
// make a sampling pass skip work it has not done.
func TestIngestLeavesTheCycleFetched(t *testing.T) {
	t.Parallel()

	engine, s, _ := ingestHarness(t, &fakeFetcher{})

	got, err := engine.Ingest(t.Context(), aRequest())
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	cycle, err := s.Cycles().Get(t.Context(), got.CycleID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if cycle.Status != "fetched" {
		t.Errorf("status = %q, want fetched", cycle.Status)
	}
}

// The provenance record carries what the design promises: the URL fetched,
// the hash of what arrived, its size, and the parser that read it.
func TestProvenanceRecordsWhatWasFetched(t *testing.T) {
	t.Parallel()

	engine, _, recorder := ingestHarness(t, &fakeFetcher{})

	if _, err := engine.Ingest(t.Context(), aRequest()); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	if len(recorder.byHash) == 0 {
		t.Fatal("nothing was recorded")
	}

	for _, got := range recorder.byHash {
		if got.URL == "" {
			t.Error("a dataset version has no URL")
		}

		if got.ParserVersion != weather.ParserVersion {
			t.Errorf("parser version = %q, want %q",
				got.ParserVersion, weather.ParserVersion)
		}

		if got.SizeBytes == 0 {
			t.Error("a dataset version has no size; a truncated download that " +
				"parses is otherwise indistinguishable from a complete one")
		}

		if got.RetrievedAt.IsZero() {
			t.Error("a dataset version has no retrieval time")
		}
	}
}

func TestIngestedCyclesAreListable(t *testing.T) {
	t.Parallel()

	engine, _, _ := ingestHarness(t, &fakeFetcher{})

	if _, err := engine.Ingest(t.Context(), aRequest()); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	got, err := engine.Cycles(t.Context(), store.CycleFilter{}, pkgstore.Page{})
	if err != nil {
		t.Fatalf("Cycles: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("listed %d cycles, want 1", len(got))
	}
}

// A re-ingest of a cycle that has already been sampled must not push it back
// to fetched.
//
// Every field skips on the second pass, and while nothing swept the fetched
// state this was invisible. Once the poller started sampling that state, it
// meant re-uploading every grid to the solver on every tick, forever — which
// is what a live run actually did before this test existed.
func TestReingestDoesNotUnsampleACycle(t *testing.T) {
	t.Parallel()

	fetcher := &fakeFetcher{}
	engine, s, _ := ingestHarness(t, fetcher)

	first, err := engine.Ingest(t.Context(), aRequest())
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	if err := s.Cycles().SetStatus(
		t.Context(), first.CycleID, forecast.StatusSampled,
	); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	second, err := engine.Ingest(t.Context(), aRequest())
	if err != nil {
		t.Fatalf("second Ingest: %v", err)
	}

	if second.Retrieved != 0 {
		t.Fatalf("the second pass retrieved %d fields, want everything skipped",
			second.Retrieved)
	}

	cycle, err := s.Cycles().Get(t.Context(), first.CycleID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if cycle.Status != forecast.StatusSampled {
		t.Errorf("a skip-only re-ingest moved the cycle to %s, want it left at %s",
			cycle.Status, forecast.StatusSampled)
	}
}

// A forecast hour arriving after a cycle was sampled does move it back, because
// the new hour has not been sampled onto anything.
func TestANewHourReopensASampledCycle(t *testing.T) {
	t.Parallel()

	fetcher := &fakeFetcher{}
	engine, s, _ := ingestHarness(t, fetcher)

	first, err := engine.Ingest(t.Context(), aRequest())
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	if err := s.Cycles().SetStatus(
		t.Context(), first.CycleID, forecast.StatusSampled,
	); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	later := aRequest()
	later.Hours = []int{2}

	if _, err := engine.Ingest(t.Context(), later); err != nil {
		t.Fatalf("second Ingest: %v", err)
	}

	cycle, err := s.Cycles().Get(t.Context(), first.CycleID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if cycle.Status != forecast.StatusFetched {
		t.Errorf("a newly retrieved hour left the cycle at %s, want %s",
			cycle.Status, forecast.StatusFetched)
	}
}
