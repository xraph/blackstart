package memory_test

import (
	"testing"
	"time"

	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/hazard/conductor"
	hid "github.com/xraph/blackstart/extensions/hazard/id"
	"github.com/xraph/blackstart/extensions/hazard/store"
	"github.com/xraph/blackstart/extensions/hazard/store/memory"
	"github.com/xraph/blackstart/pkg/id"
)

// The composite has to satisfy the interface, checked here rather than by an
// unused variable so that the failure names this file.
var _ store.Store = (*memory.Store)(nil)

func issuance() time.Time { return time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC) }

func assignment(branch id.ID) conductor.Assignment {
	return conductor.Assignment{
		ID:            hid.NewAssignment(),
		CarrierID:     branch,
		ConductorCode: "drake",
		Basis:         conductor.Surveyed,
	}
}

func value(span id.ID, q store.Quantity, hour int, v float64) store.SpanValue {
	return store.SpanValue{
		SpanID:   span,
		Quantity: q,
		IssuedAt: issuance(),
		ValidAt:  issuance().Add(time.Duration(hour) * time.Hour),
		Value:    v,
	}
}

func TestLifecycleIsInert(t *testing.T) {
	t.Parallel()

	s := memory.New()

	if err := s.Migrate(t.Context()); err != nil {
		t.Errorf("Migrate: %v", err)
	}

	if err := s.Ping(t.Context()); err != nil {
		t.Errorf("Ping: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	if s.StoreName() == "" {
		t.Error("the store has no name for health output")
	}
}

func newSpan(t *testing.T) id.ID {
	t.Helper()

	return id.MustNew(gid.Span)
}
