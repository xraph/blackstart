package memory_test

import (
	"errors"
	"testing"
	"time"

	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/weather/field"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	wid "github.com/xraph/blackstart/extensions/weather/id"
	"github.com/xraph/blackstart/extensions/weather/observation"
	"github.com/xraph/blackstart/extensions/weather/store"
	"github.com/xraph/blackstart/extensions/weather/store/memory"
	"github.com/xraph/blackstart/pkg/bserr"
)

func TestSampleRoundTrip(t *testing.T) {
	t.Parallel()

	s := memory.New()
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

	if err := s.Samples().WriteForecast(t.Context(), samples); err != nil {
		t.Fatalf("WriteForecast: %v", err)
	}

	key := forecast.SampleKey(span, field.Gust10m, issuance)

	got, err := s.Samples().ReadForecast(
		t.Context(), []string{key}, issuance, issuance.Add(6*time.Hour))
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

	// Issuance must survive the round trip through the key, since it is the
	// only place it is stored.
	if !got[0].IssuedAt.Equal(issuance) {
		t.Errorf("issuance = %s, want %s", got[0].IssuedAt, issuance)
	}
}

// The window is half-open: a sample exactly at `to` belongs to the next
// window, or a caller walking a series hour by hour reads every boundary
// twice.
func TestReadForecastWindowIsHalfOpen(t *testing.T) {
	t.Parallel()

	s := memory.New()
	span := gid.NewSpan()

	at := issuance.Add(time.Hour)

	if err := s.Samples().WriteForecast(t.Context(), []forecast.Sample{{
		SpanID: span, Field: field.Gust10m,
		IssuedAt: issuance, ValidAt: at, Value: 14.2,
	}}); err != nil {
		t.Fatalf("WriteForecast: %v", err)
	}

	key := forecast.SampleKey(span, field.Gust10m, issuance)

	got, err := s.Samples().ReadForecast(t.Context(), []string{key}, issuance, at)
	if err != nil {
		t.Fatalf("ReadForecast: %v", err)
	}

	if len(got) != 0 {
		t.Errorf("a sample exactly at `to` was included; the window must be [from, to)")
	}

	got, err = s.Samples().ReadForecast(
		t.Context(), []string{key}, at, at.Add(time.Second))
	if err != nil {
		t.Fatalf("ReadForecast: %v", err)
	}

	if len(got) != 1 {
		t.Errorf("a sample exactly at `from` was excluded; the window must be [from, to)")
	}
}

// Samples from two issuances of the same span and field are different series,
// which is the whole reason issuance is in the key.
func TestForecastSeriesSeparateByIssuance(t *testing.T) {
	t.Parallel()

	s := memory.New()
	span := gid.NewSpan()
	later := issuance.Add(time.Hour)
	valid := issuance.Add(6 * time.Hour)

	if err := s.Samples().WriteForecast(t.Context(), []forecast.Sample{
		{SpanID: span, Field: field.Gust10m, IssuedAt: issuance, ValidAt: valid, Value: 14.2},
		{SpanID: span, Field: field.Gust10m, IssuedAt: later, ValidAt: valid, Value: 21.0},
	}); err != nil {
		t.Fatalf("WriteForecast: %v", err)
	}

	got, err := s.Samples().ReadForecast(
		t.Context(),
		[]string{forecast.SampleKey(span, field.Gust10m, later)},
		issuance, valid.Add(time.Hour))
	if err != nil {
		t.Fatalf("ReadForecast: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("the later issuance returned %d samples, want 1", len(got))
	}

	if got[0].Value != 21.0 {
		t.Errorf("value = %v, want the later issuance's 21.0", got[0].Value)
	}
}

func TestWriteForecastRejectsAnInvalidSample(t *testing.T) {
	t.Parallel()

	s := memory.New()

	err := s.Samples().WriteForecast(t.Context(), []forecast.Sample{{
		SpanID: gid.NewSpan(), Field: field.Gust10m,
		IssuedAt: issuance, ValidAt: issuance.Add(-time.Hour), Value: 14.2,
	}})
	if !errors.Is(err, bserr.ErrInvalid) {
		t.Fatalf("error = %v, want bserr.ErrInvalid", err)
	}
}

func TestObservationRoundTrip(t *testing.T) {
	t.Parallel()

	s := memory.New()
	station := wid.NewStation()

	if err := s.Samples().WriteObservations(t.Context(), []observation.Observation{
		{StationID: station, Field: field.Temp2m, ObservedAt: issuance, Value: 305.4},
	}); err != nil {
		t.Fatalf("WriteObservations: %v", err)
	}

	got, err := s.Samples().ReadObservations(
		t.Context(),
		[]string{observation.SampleKey(station, field.Temp2m)},
		issuance, issuance.Add(time.Hour))
	if err != nil {
		t.Fatalf("ReadObservations: %v", err)
	}

	if len(got) != 1 || got[0].Value != 305.4 {
		t.Fatalf("ReadObservations returned %+v, want one reading of 305.4", got)
	}
}

// Samples completes the composite, so this is where the interface assertion
// belongs. Without it the memory store could drift from the Fabriq one and
// every engine test written against this would prove nothing about
// production.
var _ store.Store = (*memory.Store)(nil)
