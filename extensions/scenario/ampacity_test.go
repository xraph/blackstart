package scenario

import (
	"context"
	"testing"
	"time"

	gid "github.com/xraph/blackstart/extensions/geo/id"
	hazardstore "github.com/xraph/blackstart/extensions/hazard/store"
	"github.com/xraph/blackstart/pkg/id"
)

// fakeSeries records the read it was asked for and answers with values it was
// given, whatever window was requested. Deliberately ignoring the window is
// what lets a test see that the adapter still refuses a value from another
// forecast hour — a fake that filtered would be asserting its own behavior.
type fakeSeries struct {
	values []hazardstore.SpanValue
	err    error

	keys     []string
	from, to time.Time
	calls    int
}

func (f *fakeSeries) Read(
	_ context.Context, keys []string, from, to time.Time,
) ([]hazardstore.SpanValue, error) {
	f.calls++
	f.keys, f.from, f.to = keys, from, to

	if f.err != nil {
		return nil, f.err
	}

	return f.values, nil
}

func ampacityAt(span id.ID, at time.Time, amperes float64) hazardstore.SpanValue {
	return hazardstore.SpanValue{
		SpanID:   span,
		Quantity: hazardstore.QuantityAmpacity,
		IssuedAt: issued,
		ValidAt:  at,
		Value:    amperes,
	}
}

func spanAmpacities(t *testing.T, series SeriesReader) *SpanAmpacities {
	t.Helper()

	reader, err := NewSpanAmpacities(series)
	if err != nil {
		t.Fatalf("NewSpanAmpacities: %v", err)
	}

	return reader
}

// One read for every span, keyed by (span, ampacity, issuance) — the series
// identity hazard stores under. A key composed any other way reads an empty
// series and reports a network with no weather on it.
func TestAmpacitiesAsksForOneSeriesPerSpan(t *testing.T) {
	t.Parallel()

	first, second := id.MustNew(gid.Span), id.MustNew(gid.Span)
	series := &fakeSeries{values: []hazardstore.SpanValue{
		ampacityAt(first, valid, 900),
		ampacityAt(second, valid, 1100),
	}}

	got, err := spanAmpacities(t, series).Ampacities(
		t.Context(), []id.ID{first, second}, issued, valid)
	if err != nil {
		t.Fatalf("Ampacities: %v", err)
	}

	if series.calls != 1 {
		t.Errorf("%d reads for two spans, want 1 — the store takes a key list", series.calls)
	}

	want := []string{
		hazardstore.ValueKey(first, hazardstore.QuantityAmpacity, issued),
		hazardstore.ValueKey(second, hazardstore.QuantityAmpacity, issued),
	}

	for index, key := range want {
		if series.keys[index] != key {
			t.Errorf("key %d = %q, want %q", index, series.keys[index], key)
		}
	}

	if got[first] != 900 || got[second] != 1100 {
		t.Errorf("read back %v", got)
	}
}

// The window is the pinned instant and nothing wider. A hazard series holds
// one value per forecast hour under one key, so a window covering an hour
// returns several and something downstream picks among them silently — the
// wrong-forecast-hour failure both time axes exist to prevent.
func TestTheWindowIsTheExactInstant(t *testing.T) {
	t.Parallel()

	span := id.MustNew(gid.Span)
	series := &fakeSeries{values: []hazardstore.SpanValue{
		ampacityAt(span, valid, 900),
	}}

	if _, err := spanAmpacities(t, series).Ampacities(
		t.Context(), []id.ID{span}, issued, valid); err != nil {
		t.Fatalf("Ampacities: %v", err)
	}

	if !series.from.Equal(valid) {
		t.Errorf("the window starts at %v, want the pinned %v", series.from, valid)
	}

	// Exactly one microsecond: the resolution of the TIMESTAMPTZ column the
	// series lives in. Narrower and both bounds land on the same value on the
	// wire, so `time >= X AND time < X` selects nothing and every span comes
	// back unrated. Wider and the next forecast hour is in range.
	if width := series.to.Sub(series.from); width != time.Microsecond {
		t.Errorf("the window is %v wide, want exactly %v — the resolution of the "+
			"column it is read from", width, time.Microsecond)
	}
}

// A valid time finer than the column can store still finds its row. The
// driver truncates on the way in, so a window starting at the untruncated
// value would begin after the row it is looking for.
func TestASubMicrosecondValidTimeStillMatches(t *testing.T) {
	t.Parallel()

	span := id.MustNew(gid.Span)
	stored := valid.Add(3 * time.Microsecond)
	asked := stored.Add(500 * time.Nanosecond)

	series := &fakeSeries{values: []hazardstore.SpanValue{ampacityAt(span, stored, 900)}}

	got, err := spanAmpacities(t, series).Ampacities(t.Context(), []id.ID{span}, issued, asked)
	if err != nil {
		t.Fatalf("Ampacities: %v", err)
	}

	if got[span] != 900 {
		t.Errorf("got %v, want the value stored at the microsecond this instant "+
			"truncates to", got)
	}

	if !series.from.Equal(stored) {
		t.Errorf("the window starts at %v, want the truncated %v", series.from, stored)
	}
}

// A value the store returns for another moment is not the value that was
// asked for. Never the nearest sample, never the enclosing hour: a rating
// from the wrong forecast hour is a real number for a real moment, and
// nothing downstream can tell it apart from the right one.
func TestAValueFromAnotherMomentIsNotUsed(t *testing.T) {
	t.Parallel()

	span := id.MustNew(gid.Span)
	series := &fakeSeries{values: []hazardstore.SpanValue{
		ampacityAt(span, valid.Add(time.Hour), 900),
	}}

	got, err := spanAmpacities(t, series).Ampacities(
		t.Context(), []id.ID{span}, issued, valid)
	if err != nil {
		t.Fatalf("Ampacities: %v", err)
	}

	if _, present := got[span]; present {
		t.Errorf("the span was rated from a value an hour off the pinned instant: %v", got)
	}
}

// A span hazard computed nothing for is absent, not zero. Zero would become
// this branch's rating, and every consumer reads a non-positive rating as
// unrated and skips the branch — removing it from every violation check
// rather than failing one.
func TestASpanWithNoValueIsAbsent(t *testing.T) {
	t.Parallel()

	rated, unrated := id.MustNew(gid.Span), id.MustNew(gid.Span)
	series := &fakeSeries{values: []hazardstore.SpanValue{ampacityAt(rated, valid, 900)}}

	got, err := spanAmpacities(t, series).Ampacities(
		t.Context(), []id.ID{rated, unrated}, issued, valid)
	if err != nil {
		t.Fatalf("Ampacities: %v", err)
	}

	if _, present := got[unrated]; present {
		t.Errorf("a span with no computed value came back as %v", got[unrated])
	}

	if len(got) != 1 {
		t.Errorf("got %d values for one rated span", len(got))
	}
}

// A store that cannot answer fails the run. Absence is the map and failure is
// the error; collapsing the two turns a saturated connection pool into a
// network quietly left on nameplate ratings.
func TestAStoreThatCannotAnswerFailsTheRun(t *testing.T) {
	t.Parallel()

	series := &fakeSeries{err: errBroken}

	_, err := spanAmpacities(t, series).Ampacities(
		t.Context(), []id.ID{id.MustNew(gid.Span)}, issued, valid)
	if err == nil {
		t.Fatal("want an error: the store could not answer")
	}
}

// Two disagreeing values for one span at one instant are an ambiguity, not a
// choice. hazard's memory store appends without upsert and neither store
// orders values sharing a timestamp, so picking one would make the same
// pinned manifest rate differently on different runs.
func TestTwoDisagreeingValuesAtOneInstantAreRefused(t *testing.T) {
	t.Parallel()

	span := id.MustNew(gid.Span)
	series := &fakeSeries{values: []hazardstore.SpanValue{
		ampacityAt(span, valid, 900),
		ampacityAt(span, valid, 1400),
	}}

	_, err := spanAmpacities(t, series).Ampacities(t.Context(), []id.ID{span}, issued, valid)
	if err == nil {
		t.Fatal("want an error: one span, one instant, two different ampacities")
	}
}

// The same number written twice is not an ambiguity. A recomputed pass that
// produced the identical value leaves nothing to choose between.
func TestTwoIdenticalValuesAtOneInstantAreFine(t *testing.T) {
	t.Parallel()

	span := id.MustNew(gid.Span)
	series := &fakeSeries{values: []hazardstore.SpanValue{
		ampacityAt(span, valid, 900),
		ampacityAt(span, valid, 900),
	}}

	got, err := spanAmpacities(t, series).Ampacities(t.Context(), []id.ID{span}, issued, valid)
	if err != nil {
		t.Fatalf("Ampacities: %v", err)
	}

	if got[span] != 900 {
		t.Errorf("got %v A, want 900", got[span])
	}
}

// No spans is no round trip. A branch whose route has no spans is a gap in
// the asset record, and asking the database about an empty list costs a query
// per such branch across a whole network.
func TestNoSpansCostsNoRead(t *testing.T) {
	t.Parallel()

	series := &fakeSeries{}

	got, err := spanAmpacities(t, series).Ampacities(t.Context(), nil, issued, valid)
	if err != nil {
		t.Fatalf("Ampacities: %v", err)
	}

	if series.calls != 0 {
		t.Errorf("%d reads for no spans, want none", series.calls)
	}

	if len(got) != 0 {
		t.Errorf("got %v for no spans", got)
	}
}
