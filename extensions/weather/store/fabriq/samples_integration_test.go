//go:build integration

package fabriq_test

import (
	"context"
	"testing"
	"time"

	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/weather/field"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	wid "github.com/xraph/blackstart/extensions/weather/id"
	"github.com/xraph/blackstart/extensions/weather/observation"
	wxstore "github.com/xraph/blackstart/extensions/weather/store"
	wxfabriq "github.com/xraph/blackstart/extensions/weather/store/fabriq"
)

// The assertion this whole design turns on: BulkWrite's column list is
// hardcoded in fabriq's adapter, so a hypertable whose shape disagrees fails
// every write. Nothing but a real database can catch that.
func TestForecastSampleRoundTrip(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	span := gid.NewSpan()

	samples := []forecast.Sample{
		{
			SpanID: span, Field: field.Gust10m,
			IssuedAt: issuance, ValidAt: issuance.Add(time.Hour), Value: 14.2,
		},
		{
			SpanID: span, Field: field.Gust10m,
			IssuedAt: issuance, ValidAt: issuance.Add(2 * time.Hour), Value: 18.9,
		},
	}

	if err := s.Samples().WriteForecast(ctx, samples); err != nil {
		t.Fatalf("WriteForecast: %v", err)
	}

	key := forecast.SampleKey(span, field.Gust10m, issuance)

	got, err := s.Samples().ReadForecast(
		ctx, []string{key}, issuance, issuance.Add(6*time.Hour))
	if err != nil {
		t.Fatalf("ReadForecast: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("ReadForecast returned %d samples, want 2", len(got))
	}

	if got[0].Value != 14.2 || got[1].Value != 18.9 {
		t.Errorf("values = %v, %v; want 14.2 then 18.9 in valid-time order",
			got[0].Value, got[1].Value)
	}

	// Issuance survives only through the series key, so this is the assertion
	// that the key encoding actually works end to end.
	if !got[0].IssuedAt.Equal(issuance) {
		t.Errorf("issuance = %s, want %s", got[0].IssuedAt, issuance)
	}

	if got[0].SpanID != span || got[0].Field != field.Gust10m {
		t.Errorf("span/field = %s/%s, want %s/%s",
			got[0].SpanID, got[0].Field, span, field.Gust10m)
	}
}

// Two issuances of the same span and field are separate series, which is the
// whole reason issuance is in the key.
func TestForecastSeriesSeparateByIssuance(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	span := gid.NewSpan()
	later := issuance.Add(time.Hour)
	valid := issuance.Add(6 * time.Hour)

	if err := s.Samples().WriteForecast(ctx, []forecast.Sample{
		{SpanID: span, Field: field.Gust10m, IssuedAt: issuance, ValidAt: valid, Value: 14.2},
		{SpanID: span, Field: field.Gust10m, IssuedAt: later, ValidAt: valid, Value: 21.0},
	}); err != nil {
		t.Fatalf("WriteForecast: %v", err)
	}

	got, err := s.Samples().ReadForecast(ctx,
		[]string{forecast.SampleKey(span, field.Gust10m, later)},
		issuance, valid.Add(time.Hour))
	if err != nil {
		t.Fatalf("ReadForecast: %v", err)
	}

	if len(got) != 1 || got[0].Value != 21.0 {
		t.Fatalf("got %+v, want only the later issuance's 21.0", got)
	}
}

// The window is half-open in SQL as well as in memory, or a caller walking a
// series hour by hour reads every boundary twice.
func TestReadForecastWindowIsHalfOpen(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	span := gid.NewSpan()
	at := issuance.Add(time.Hour)

	if err := s.Samples().WriteForecast(ctx, []forecast.Sample{{
		SpanID: span, Field: field.Gust10m,
		IssuedAt: issuance, ValidAt: at, Value: 14.2,
	}}); err != nil {
		t.Fatalf("WriteForecast: %v", err)
	}

	key := forecast.SampleKey(span, field.Gust10m, issuance)

	got, err := s.Samples().ReadForecast(ctx, []string{key}, issuance, at)
	if err != nil {
		t.Fatalf("ReadForecast: %v", err)
	}

	if len(got) != 0 {
		t.Error("a sample exactly at `to` was included; the window must be [from, to)")
	}

	got, err = s.Samples().ReadForecast(
		ctx, []string{key}, at, at.Add(time.Second))
	if err != nil {
		t.Fatalf("ReadForecast: %v", err)
	}

	if len(got) != 1 {
		t.Error("a sample exactly at `from` was excluded; the window must be [from, to)")
	}
}

// Several keys in one query is the reason readSeries exists rather than the
// port's single-key Range.
func TestReadForecastAcrossManyKeys(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	span := gid.NewSpan()
	valid := issuance.Add(time.Hour)

	if err := s.Samples().WriteForecast(ctx, []forecast.Sample{
		{SpanID: span, Field: field.Gust10m, IssuedAt: issuance, ValidAt: valid, Value: 14.2},
		{SpanID: span, Field: field.Temp2m, IssuedAt: issuance, ValidAt: valid, Value: 305.4},
		{SpanID: span, Field: field.CAPE, IssuedAt: issuance, ValidAt: valid, Value: 1200},
	}); err != nil {
		t.Fatalf("WriteForecast: %v", err)
	}

	keys := []string{
		forecast.SampleKey(span, field.Gust10m, issuance),
		forecast.SampleKey(span, field.Temp2m, issuance),
		forecast.SampleKey(span, field.CAPE, issuance),
	}

	got, err := s.Samples().ReadForecast(ctx, keys, issuance, valid.Add(time.Hour))
	if err != nil {
		t.Fatalf("ReadForecast: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("ReadForecast over 3 keys returned %d samples, want 3", len(got))
	}
}

func TestObservationRoundTrip(t *testing.T) {
	s := harness(t)
	ctx := context.Background()

	station := wid.NewStation()

	if err := s.Samples().WriteObservations(ctx, []observation.Observation{
		{StationID: station, Field: field.Temp2m, ObservedAt: issuance, Value: 305.4},
	}); err != nil {
		t.Fatalf("WriteObservations: %v", err)
	}

	got, err := s.Samples().ReadObservations(ctx,
		[]string{observation.SampleKey(station, field.Temp2m)},
		issuance, issuance.Add(time.Hour))
	if err != nil {
		t.Fatalf("ReadObservations: %v", err)
	}

	if len(got) != 1 || got[0].Value != 305.4 {
		t.Fatalf("got %+v, want one reading of 305.4", got)
	}

	if got[0].StationID != station || got[0].Field != field.Temp2m {
		t.Errorf("station/field = %s/%s, want %s/temp2m",
			got[0].StationID, got[0].Field, station)
	}
}

func TestPing(t *testing.T) {
	s := harness(t)

	if err := s.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

var _ wxstore.Store = (*wxfabriq.Store)(nil)
