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
