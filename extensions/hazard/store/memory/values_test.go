package memory_test

import (
	"testing"
	"time"

	"github.com/xraph/blackstart/extensions/hazard/store"
	"github.com/xraph/blackstart/extensions/hazard/store/memory"
	"github.com/xraph/blackstart/pkg/id"
)

func TestValuesRoundTrip(t *testing.T) {
	t.Parallel()

	s := memory.New()
	span := newSpan(t)

	want := []store.SpanValue{
		value(span, store.QuantityAmpacity, 0, 1200),
		value(span, store.QuantityAmpacity, 1, 1150),
		value(span, store.QuantityAmpacity, 2, 980),
	}

	if err := s.Values().Write(t.Context(), want); err != nil {
		t.Fatalf("Write: %v", err)
	}

	key := store.ValueKey(span, store.QuantityAmpacity, issuance())

	got, err := s.Values().Read(
		t.Context(), []string{key}, issuance(), issuance().Add(6*time.Hour))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("read %d values, wrote %d", len(got), len(want))
	}

	for index, one := range got {
		if one != want[index] {
			t.Errorf("value %d read back %+v, wrote %+v", index, one, want[index])
		}
	}
}

// Half-open, so a caller walking a series hour by hour does not read every
// boundary twice.
func TestTheReadWindowIsHalfOpen(t *testing.T) {
	t.Parallel()

	s := memory.New()
	span := newSpan(t)

	if err := s.Values().Write(t.Context(), []store.SpanValue{
		value(span, store.QuantityWindLoad, 0, 10),
		value(span, store.QuantityWindLoad, 1, 20),
		value(span, store.QuantityWindLoad, 2, 30),
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	key := store.ValueKey(span, store.QuantityWindLoad, issuance())

	got, err := s.Values().Read(
		t.Context(), []string{key}, issuance(), issuance().Add(2*time.Hour))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("read %d values over a two-hour half-open window, want 2", len(got))
	}

	if !got[0].ValidAt.Equal(issuance()) {
		t.Errorf("the window excluded its own lower bound")
	}
}

// Quantities are separate series even for the same span and issuance. A key
// that dropped the quantity would blend a rating in amperes with a load in
// newtons per meter and read back as one plausible-looking series.
func TestQuantitiesAreSeparateSeries(t *testing.T) {
	t.Parallel()

	s := memory.New()
	span := newSpan(t)

	if err := s.Values().Write(t.Context(), []store.SpanValue{
		value(span, store.QuantityAmpacity, 0, 1200),
		value(span, store.QuantityWindLoad, 0, 7),
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := s.Values().Read(
		t.Context(),
		[]string{store.ValueKey(span, store.QuantityAmpacity, issuance())},
		issuance(), issuance().Add(time.Hour))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(got) != 1 || got[0].Value != 1200 {
		t.Errorf("reading the ampacity series gave %+v", got)
	}
}

// Issuance is part of the key, so a re-issued forecast writes a new series
// rather than overwriting the one a downstream consumer may already hold.
func TestIssuanceSeparatesSeries(t *testing.T) {
	t.Parallel()

	s := memory.New()
	span := newSpan(t)
	later := issuance().Add(6 * time.Hour)

	if err := s.Values().Write(t.Context(), []store.SpanValue{
		value(span, store.QuantityAmpacity, 0, 1200),
		{
			SpanID: span, Quantity: store.QuantityAmpacity,
			IssuedAt: later, ValidAt: issuance(), Value: 1400,
		},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := s.Values().Read(
		t.Context(),
		[]string{store.ValueKey(span, store.QuantityAmpacity, later)},
		issuance(), issuance().Add(time.Hour))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(got) != 1 || got[0].Value != 1400 {
		t.Errorf("the later issuance read back %+v", got)
	}
}

// A bad value fails the whole batch rather than leaving half of it stored,
// matching BulkWrite's one multi-row INSERT.
func TestABadValueFailsTheWholeBatch(t *testing.T) {
	t.Parallel()

	s := memory.New()
	span := newSpan(t)

	err := s.Values().Write(t.Context(), []store.SpanValue{
		value(span, store.QuantityAmpacity, 0, 1200),
		value(span, "vibes", 1, 1),
	})
	if err == nil {
		t.Fatal("a value with an unknown quantity was stored")
	}

	got, readErr := s.Values().Read(
		t.Context(),
		[]string{store.ValueKey(span, store.QuantityAmpacity, issuance())},
		issuance(), issuance().Add(6*time.Hour))
	if readErr != nil {
		t.Fatalf("Read: %v", readErr)
	}

	if len(got) != 0 {
		t.Errorf("the good half of a rejected batch was stored: %+v", got)
	}
}

func TestReadRefusesAMalformedKey(t *testing.T) {
	t.Parallel()

	s := memory.New()

	for _, key := range []string{
		"",
		"nonsense",
		id.MustNew("brch").String() + "|ampacity|20260803T1200Z",
		newSpan(t).String() + "|vibes|20260803T1200Z",
		newSpan(t).String() + "|ampacity|yesterday",
	} {
		if _, err := s.Values().Read(
			t.Context(), []string{key}, issuance(), issuance().Add(time.Hour),
		); err == nil {
			t.Errorf("%q: accepted", key)
		}
	}
}
