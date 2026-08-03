package memory_test

import (
	"errors"
	"testing"

	"github.com/xraph/blackstart/extensions/geo/shape"
	wid "github.com/xraph/blackstart/extensions/weather/id"
	"github.com/xraph/blackstart/extensions/weather/observation"
	"github.com/xraph/blackstart/extensions/weather/store/memory"
	"github.com/xraph/blackstart/pkg/bserr"
)

func TestStationByCode(t *testing.T) {
	t.Parallel()

	s := memory.New()

	want := observation.Station{
		ID:       wid.NewStation(),
		Code:     "KAUS",
		Name:     "Austin-Bergstrom",
		Location: shape.Point{Lon: -97.67, Lat: 30.19},
	}

	if err := s.Stations().Put(t.Context(), want); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Stations().ByCode(t.Context(), "KAUS")
	if err != nil {
		t.Fatalf("ByCode: %v", err)
	}

	if got.ID != want.ID {
		t.Errorf("ByCode returned %s, want %s", got.ID, want.ID)
	}

	if _, err := s.Stations().ByCode(t.Context(), "KXXX"); !errors.Is(err, bserr.ErrNotFound) {
		t.Errorf("ByCode(missing) error = %v, want ErrNotFound", err)
	}
}

// The station list is re-seeded from the publisher, so a repeat write of an
// unchanged row must not look like a failure.
func TestStationPutReplaces(t *testing.T) {
	t.Parallel()

	s := memory.New()

	station := observation.Station{
		ID:       wid.NewStation(),
		Code:     "KAUS",
		Name:     "Austin",
		Location: shape.Point{Lon: -97.67, Lat: 30.19},
	}

	if err := s.Stations().Put(t.Context(), station); err != nil {
		t.Fatalf("first Put: %v", err)
	}

	station.Name = "Austin-Bergstrom International"

	if err := s.Stations().Put(t.Context(), station); err != nil {
		t.Fatalf("second Put: %v", err)
	}

	got, err := s.Stations().Get(t.Context(), station.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Name != "Austin-Bergstrom International" {
		t.Errorf("name = %q; the second Put did not replace", got.Name)
	}
}
