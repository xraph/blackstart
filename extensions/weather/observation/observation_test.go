package observation_test

import (
	"errors"
	"testing"
	"time"

	"github.com/xraph/blackstart/extensions/geo/shape"
	"github.com/xraph/blackstart/extensions/weather/field"
	wid "github.com/xraph/blackstart/extensions/weather/id"
	"github.com/xraph/blackstart/extensions/weather/observation"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

func TestStationValidation(t *testing.T) {
	t.Parallel()

	valid := observation.Station{
		ID:       wid.NewStation(),
		Code:     "KAUS",
		Name:     "Austin-Bergstrom",
		Location: shape.Point{Lon: -97.67, Lat: 30.19},
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("a well-formed station was rejected: %v", err)
	}

	for name, mutate := range map[string]func(*observation.Station){
		"no identifier": func(s *observation.Station) { s.ID = id.Nil },
		"no code":       func(s *observation.Station) { s.Code = "" },
		"blank code":    func(s *observation.Station) { s.Code = "   " },
		// Latitude beyond ±90 is the signature of transposed coordinates,
		// which is the mistake worth catching at the boundary.
		"transposed coordinates": func(s *observation.Station) {
			s.Location = shape.Point{Lon: 30.19, Lat: -97.67}
		},
	} {
		broken := valid
		mutate(&broken)

		if err := broken.Validate(); err == nil {
			t.Errorf("%s: validated", name)
		}
	}
}

func TestObservationValidation(t *testing.T) {
	t.Parallel()

	valid := observation.Observation{
		StationID:  wid.NewStation(),
		Field:      field.Gust10m,
		ObservedAt: time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC),
		Value:      14.2,
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("a well-formed observation was rejected: %v", err)
	}

	for name, mutate := range map[string]func(*observation.Observation){
		"no station":    func(o *observation.Observation) { o.StationID = id.Nil },
		"unknown field": func(o *observation.Observation) { o.Field = "gust_10m" },
		"no time":       func(o *observation.Observation) { o.ObservedAt = time.Time{} },
	} {
		broken := valid
		mutate(&broken)

		err := broken.Validate()
		if err == nil {
			t.Errorf("%s: validated", name)

			continue
		}

		if !errors.Is(err, bserr.ErrInvalid) {
			t.Errorf("%s: error = %v, want bserr.ErrInvalid", name, err)
		}
	}
}

// An observation's key carries no issuance: for an observation, valid time and
// knowledge time are the same instant. Two parts, not three with one blank —
// a trailing empty part would sort and prefix-match differently.
func TestSampleKeyHasTwoParts(t *testing.T) {
	t.Parallel()

	station := wid.NewStation()

	got := observation.SampleKey(station, field.Gust10m)
	want := station.String() + "|" + string(field.Gust10m)

	if got != want {
		t.Errorf("SampleKey = %q, want %q", got, want)
	}
}

func TestSampleKeyDiffersPerField(t *testing.T) {
	t.Parallel()

	station := wid.NewStation()
	seen := map[string]field.Field{}

	for _, f := range field.Fields() {
		key := observation.SampleKey(station, f)

		if other, clash := seen[key]; clash {
			t.Errorf("%q and %q produced the same key %q", f, other, key)
		}

		seen[key] = f
	}
}
