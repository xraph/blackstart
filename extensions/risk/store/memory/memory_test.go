package memory_test

import (
	"context"
	"testing"
	"time"

	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/risk/store"
	"github.com/xraph/blackstart/extensions/risk/store/memory"
)

func TestValuesComeBackInTimeOrder(t *testing.T) {
	st := memory.New()
	ctx := context.Background()
	span := gid.NewSpan()
	issued := time.Date(2026, 2, 15, 18, 0, 0, 0, time.UTC)

	// Written out of order on purpose.
	in := []store.ComponentValue{
		{
			ComponentID: span, Quantity: store.QuantityConductorFailure,
			IssuedAt: issued, ValidAt: issued.Add(2 * time.Hour), Probability: 0.3, Scalar: 1,
		},
		{
			ComponentID: span, Quantity: store.QuantityConductorFailure,
			IssuedAt: issued, ValidAt: issued, Probability: 0.1, Scalar: 1,
		},
		{
			ComponentID: span, Quantity: store.QuantityConductorFailure,
			IssuedAt: issued, ValidAt: issued.Add(time.Hour), Probability: 0.2, Scalar: 1,
		},
	}

	if err := st.Values().PutBatch(ctx, in); err != nil {
		t.Fatalf("PutBatch: %v", err)
	}

	back, err := st.Values().ForComponent(ctx, span,
		[]store.Quantity{store.QuantityConductorFailure},
		issued, issued, issued.Add(6*time.Hour))
	if err != nil {
		t.Fatalf("ForComponent: %v", err)
	}

	if len(back) != 3 {
		t.Fatalf("read %d values, wrote 3", len(back))
	}

	for index := 1; index < len(back); index++ {
		if !back[index].ValidAt.After(back[index-1].ValidAt) {
			t.Fatalf("value %d is not after its predecessor; a series read out "+
				"of order is not a series", index)
		}
	}
}

func TestAnotherIssuanceIsADifferentSeries(t *testing.T) {
	// The knowledge-time axis. Two forecasts of the same hour, issued six
	// hours apart, are two different claims — and a reader asking what was
	// known at the earlier issuance must not receive the later revision.
	st := memory.New()
	ctx := context.Background()
	span := gid.NewSpan()
	early := time.Date(2026, 2, 15, 12, 0, 0, 0, time.UTC)
	late := early.Add(6 * time.Hour)
	valid := early.Add(12 * time.Hour)

	if err := st.Values().PutBatch(ctx, []store.ComponentValue{
		{
			ComponentID: span, Quantity: store.QuantityConductorFailure,
			IssuedAt: early, ValidAt: valid, Probability: 0.1, Scalar: 1,
		},
		{
			ComponentID: span, Quantity: store.QuantityConductorFailure,
			IssuedAt: late, ValidAt: valid, Probability: 0.9, Scalar: 1,
		},
	}); err != nil {
		t.Fatalf("PutBatch: %v", err)
	}

	back, err := st.Values().ForComponent(ctx, span,
		[]store.Quantity{store.QuantityConductorFailure},
		early, valid.Add(-time.Hour), valid.Add(time.Hour))
	if err != nil {
		t.Fatalf("ForComponent: %v", err)
	}

	if len(back) != 1 || back[0].Probability != 0.1 {
		t.Fatalf("asking as of the early issuance returned %+v; the later "+
			"revision leaked into a question about what was known then", back)
	}
}

// TestAnIssuanceIsFoundRegardlessOfItsLocation guards against issuance key
// drift between this store and store/fabriq: values.go normalizes issued_at
// with .UTC() before using it as a query parameter, but this store's
// seriesKey held a raw time.Time, whose equality (and the map hashing it
// relies on) includes the location pointer and the monotonic reading, not
// just the instant. compute.go passes cycle.IssuedAt unnormalized, so a
// write under a non-UTC-located time.Time and a read keyed on the UTC
// equivalent of the same instant must still find each other. Every other
// fixture in this package is already a UTC constant, which is exactly why
// this gap went unnoticed until a mutation review caught it.
func TestAnIssuanceIsFoundRegardlessOfItsLocation(t *testing.T) {
	st := memory.New()
	ctx := context.Background()
	span := gid.NewSpan()

	// 13:00 at UTC-5 is the same instant as 18:00 UTC, but as a time.Time the
	// two are not ==: the location pointer differs, and struct equality (and
	// therefore map-key equality) is not the same question as "same instant".
	writtenAt := time.Date(2026, 2, 15, 13, 0, 0, 0, time.FixedZone("UTC-5", -5*60*60))
	readAt := writtenAt.UTC()

	// Deliberately struct equality, not writtenAt.Equal(readAt): the whole
	// point of this fixture is that the two are the same instant but not the
	// same struct, which is exactly what makes seriesKey's raw time.Time
	// field able to miss the match.
	if writtenAt == readAt { //nolint:staticcheck // see comment above
		t.Fatal("test fixture is not exercising the bug: writtenAt and readAt " +
			"are already struct-equal")
	}

	if err := st.Values().PutBatch(ctx, []store.ComponentValue{
		{
			ComponentID: span, Quantity: store.QuantityConductorFailure,
			IssuedAt: writtenAt, ValidAt: writtenAt, Probability: 0.42, Scalar: 1,
		},
	}); err != nil {
		t.Fatalf("PutBatch: %v", err)
	}

	back, err := st.Values().ForComponent(ctx, span,
		[]store.Quantity{store.QuantityConductorFailure},
		readAt, readAt, readAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("ForComponent: %v", err)
	}

	if len(back) != 1 {
		t.Fatalf("reading back with the UTC-normalized issuance found %d "+
			"values, want 1 — a value written under a non-UTC-located but "+
			"equal instant should still be found", len(back))
	}
}

func TestTheScalarSurvivesTheRoundTrip(t *testing.T) {
	st := memory.New()
	ctx := context.Background()
	span := gid.NewSpan()
	issued := time.Now().UTC().Truncate(time.Hour)

	if err := st.Values().PutBatch(ctx, []store.ComponentValue{
		{
			ComponentID: span, Quantity: store.QuantityConductorFailure,
			IssuedAt: issued, ValidAt: issued, Probability: 0.42, Scalar: 0.7,
		},
	}); err != nil {
		t.Fatalf("PutBatch: %v", err)
	}

	back, _ := st.Values().ForComponent(ctx, span,
		[]store.Quantity{store.QuantityConductorFailure}, issued, issued, issued.Add(time.Hour))

	if len(back) != 1 || back[0].Scalar != 0.7 {
		t.Fatalf("scalar did not survive: %+v", back)
	}
}
