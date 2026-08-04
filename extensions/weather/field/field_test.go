package field_test

import (
	"errors"
	"testing"

	"github.com/xraph/blackstart/extensions/weather/field"
	"github.com/xraph/blackstart/extensions/weather/model"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/quantity"
)

// Every field must resolve for at least one model. A field no model publishes
// is a name that can be stored and never filled.
func TestEveryFieldResolvesForSomeModel(t *testing.T) {
	t.Parallel()

	for _, f := range field.Fields() {
		var found bool

		for _, m := range model.All() {
			if _, err := field.SpecFor(f, m); err == nil {
				found = true
			}
		}

		if !found {
			t.Errorf("%q is published by no model", f)
		}
	}
}

// These were read from the section 4 of a real HRRR message on 2026-08-03,
// not from a table. GUST at surface is the one the first vocabulary got
// wrong: it was tagged height-above-ground 10 m, which no model publishes.
func TestGustIsAtSurfaceOnBothModels(t *testing.T) {
	t.Parallel()

	const (
		surface     = 1
		gustCat     = 2
		gustNum     = 22
		gustLevelIs = 0
	)

	for _, m := range model.All() {
		spec, err := field.SpecFor(field.Gust10m, m)
		if err != nil {
			t.Fatalf("SpecFor(gust10m, %s): %v", m, err)
		}

		if spec.LevelType != surface {
			t.Errorf("%s: gust levelType = %d, want %d (surface)",
				m, spec.LevelType, surface)
		}

		if spec.LevelValue != gustLevelIs {
			t.Errorf("%s: gust levelValue = %v, want 0", m, spec.LevelValue)
		}

		if spec.Category != gustCat || spec.Number != gustNum {
			t.Errorf("%s: gust category/number = %d/%d, want %d/%d",
				m, spec.Category, spec.Number, gustCat, gustNum)
		}

		if spec.Abbrev != "GUST" || spec.LevelText != "surface" {
			t.Errorf("%s: gust index key = %q:%q, want GUST:surface",
				m, spec.Abbrev, spec.LevelText)
		}
	}
}

// Wind is stored as components because a scalar speed cannot produce a
// crosswind, and geo.Span.AzimuthDeg exists precisely so a crosswind can be
// resolved against the span. Neither model publishes an instantaneous scalar
// wind anyway.
func TestWindIsStoredAsComponents(t *testing.T) {
	t.Parallel()

	for _, f := range []field.Field{
		field.WindU10m, field.WindV10m, field.WindU80m, field.WindV80m,
	} {
		spec, err := field.SpecFor(f, model.HRRR)
		if err != nil {
			t.Fatalf("SpecFor(%q): %v", f, err)
		}

		if spec.Unit.Dim != quantity.Velocity {
			t.Errorf("%q dimension = %v, want Velocity", f, spec.Unit.Dim)
		}

		if spec.Abbrev != "UGRD" && spec.Abbrev != "VGRD" {
			t.Errorf("%q abbreviation = %q, want UGRD or VGRD", f, spec.Abbrev)
		}
	}
}

// The two heights differ only by their level value, which is the whole reason
// LevelValue is on the spec.
func TestWindHeightsDifferOnlyByLevel(t *testing.T) {
	t.Parallel()

	low, err := field.SpecFor(field.WindU10m, model.HRRR)
	if err != nil {
		t.Fatalf("SpecFor(wind_u_10m): %v", err)
	}

	high, err := field.SpecFor(field.WindU80m, model.HRRR)
	if err != nil {
		t.Fatalf("SpecFor(wind_u_80m): %v", err)
	}

	if low.Category != high.Category || low.Number != high.Number ||
		low.LevelType != high.LevelType || low.Abbrev != high.Abbrev {
		t.Error("the two wind heights differ by more than their level value")
	}

	if low.LevelValue == high.LevelValue {
		t.Errorf("both heights report level %v; one is unreachable", low.LevelValue)
	}
}

// Three fields exist in HRRR and not in GFS. That is a fact about the
// products, and asking for one must fail as "this model does not publish it"
// rather than as "no such field" — the two need different handling upstream.
func TestHRRROnlyFieldsAreAbsentFromGFS(t *testing.T) {
	t.Parallel()

	for _, f := range []field.Field{
		field.Lightning, field.SnowAccum, field.FreezingRain,
	} {
		if _, err := field.SpecFor(f, model.HRRR); err != nil {
			t.Errorf("SpecFor(%q, hrrr): %v; HRRR publishes this", f, err)
		}

		_, err := field.SpecFor(f, model.GFS)
		if err == nil {
			t.Errorf("SpecFor(%q, gfs) succeeded; GFS does not publish it", f)

			continue
		}

		if !errors.Is(err, field.ErrNotPublished) {
			t.Errorf("SpecFor(%q, gfs) error = %v, want ErrNotPublished", f, err)
		}
	}
}

func TestUnknownFieldIsInvalid(t *testing.T) {
	t.Parallel()

	var unknown field.Field = "gust_10m"

	if unknown.Valid() {
		t.Fatal("a misspelled field reports valid; the vocabulary is not closed")
	}

	_, err := field.SpecFor(unknown, model.HRRR)
	if !errors.Is(err, bserr.ErrInvalid) {
		t.Fatalf("SpecFor(unknown) error = %v, want bserr.ErrInvalid", err)
	}

	// A field outside the vocabulary and a field a model does not publish are
	// different failures and must not collapse into one.
	if errors.Is(err, field.ErrNotPublished) {
		t.Error("an unknown field reported as merely unpublished")
	}
}

func TestUnknownModelIsInvalid(t *testing.T) {
	t.Parallel()

	if _, err := field.SpecFor(field.Gust10m, "ecmwf"); err == nil {
		t.Fatal("SpecFor accepted a model outside the set")
	}
}

// Precipitation type is four categorical fields as the source publishes them,
// not one derived code. A model reporting two at once on a transition
// boundary is real, and collapsing them would hide it.
func TestPrecipTypeIsFourFields(t *testing.T) {
	t.Parallel()

	want := map[field.Field]string{
		field.PrecipRain:         "CRAIN",
		field.PrecipFreezingRain: "CFRZR",
		field.PrecipIcePellets:   "CICEP",
		field.PrecipSnow:         "CSNOW",
	}

	for f, abbrev := range want {
		spec, err := field.SpecFor(f, model.HRRR)
		if err != nil {
			t.Errorf("SpecFor(%q): %v", f, err)

			continue
		}

		if spec.Abbrev != abbrev {
			t.Errorf("%q abbreviation = %q, want %q", f, spec.Abbrev, abbrev)
		}
	}
}

// Two fields sharing an index key within one model would locate the same
// message, leaving one of them never populated.
func TestIndexKeysAreUniquePerModel(t *testing.T) {
	t.Parallel()

	for _, m := range model.All() {
		seen := map[string]field.Field{}

		for _, f := range field.Fields() {
			spec, err := field.SpecFor(f, m)
			if err != nil {
				continue
			}

			key := spec.Abbrev + ":" + spec.LevelText

			if other, clash := seen[key]; clash {
				t.Errorf("%s: %q and %q both key on %q", m, f, other, key)
			}

			seen[key] = f
		}
	}
}

// The index sidecar keys on an abbreviation and a level text, not on the
// numeric coordinates. A spec missing either cannot locate its message.
func TestEverySpecCanBeLocatedInAnIndex(t *testing.T) {
	t.Parallel()

	for _, m := range model.All() {
		for _, f := range field.Fields() {
			spec, err := field.SpecFor(f, m)
			if err != nil {
				continue
			}

			if spec.Abbrev == "" || spec.LevelText == "" {
				t.Errorf("%s/%q has no index key: abbrev=%q level=%q",
					m, f, spec.Abbrev, spec.LevelText)
			}

			if spec.Unit == (quantity.Unit{}) {
				t.Errorf("%s/%q has a zero unit", m, f)
			}
		}
	}
}

func TestFieldsOrderIsStable(t *testing.T) {
	t.Parallel()

	first := field.Fields()

	for range 8 {
		next := field.Fields()

		if len(next) != len(first) {
			t.Fatalf("Fields() length changed: %d then %d", len(first), len(next))
		}

		for i := range first {
			if next[i] != first[i] {
				t.Fatalf("Fields() order changed at %d: %q then %q",
					i, first[i], next[i])
			}
		}
	}
}

func TestFieldsReturnsAFreshSlice(t *testing.T) {
	t.Parallel()

	got := field.Fields()
	got[0] = "clobbered"

	if field.Fields()[0] == "clobbered" {
		t.Fatal("Fields() shares its backing array with its callers")
	}
}
