package field_test

import (
	"errors"
	"testing"

	"github.com/xraph/blackstart/extensions/weather/field"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/quantity"
)

// A field in the vocabulary with no spec is a name that can be stored and
// never decoded, which is worse than a field that does not exist: it reads
// back as an empty series rather than as an error.
func TestEveryFieldHasASpec(t *testing.T) {
	t.Parallel()

	for _, f := range field.Fields() {
		spec, err := field.SpecFor(f)
		if err != nil {
			t.Errorf("SpecFor(%q): %v", f, err)

			continue
		}

		if spec.Field != f {
			t.Errorf("SpecFor(%q).Field = %q; the table is keyed wrong", f, spec.Field)
		}

		if spec.Unit == (quantity.Unit{}) {
			t.Errorf("SpecFor(%q) has a zero unit", f)
		}
	}
}

func TestWindFieldsCarryVelocity(t *testing.T) {
	t.Parallel()

	for _, f := range []field.Field{field.Gust10m, field.Wind10m, field.Wind80m} {
		spec, err := field.SpecFor(f)
		if err != nil {
			t.Fatalf("SpecFor(%q): %v", f, err)
		}

		if spec.Unit.Dim != quantity.Velocity {
			t.Errorf("%q dimension = %v, want Velocity", f, spec.Unit.Dim)
		}
	}
}

// The two heights share every GRIB2 coordinate but the level value, which is
// the whole reason LevelValue is on the spec.
func TestWindHeightsDifferOnlyByLevel(t *testing.T) {
	t.Parallel()

	low, err := field.SpecFor(field.Wind10m)
	if err != nil {
		t.Fatalf("SpecFor(wind10m): %v", err)
	}

	high, err := field.SpecFor(field.Wind80m)
	if err != nil {
		t.Fatalf("SpecFor(wind80m): %v", err)
	}

	if low.Discipline != high.Discipline || low.Category != high.Category ||
		low.Number != high.Number || low.LevelType != high.LevelType {
		t.Error("10 m and 80 m wind differ by more than their level value")
	}

	if low.LevelValue == high.LevelValue {
		t.Errorf("both wind heights report level %v; one of them is unreachable",
			low.LevelValue)
	}
}

func TestUnknownFieldIsInvalid(t *testing.T) {
	t.Parallel()

	var unknown field.Field = "gust_10m"

	if unknown.Valid() {
		t.Fatal("a misspelled field reports valid; the vocabulary is not closed")
	}

	_, err := field.SpecFor(unknown)
	if !errors.Is(err, bserr.ErrInvalid) {
		t.Fatalf("SpecFor(unknown) error = %v, want bserr.ErrInvalid", err)
	}
}

func TestEveryVocabularyMemberIsValid(t *testing.T) {
	t.Parallel()

	for _, f := range field.Fields() {
		if !f.Valid() {
			t.Errorf("%q is in Fields() but reports invalid", f)
		}
	}
}

// Two fields sharing a GRIB2 identity would resolve to the same message, so
// one of them would silently never be populated. This catches a collision; it
// cannot catch a coordinate that is uniquely wrong, which is what verification
// against real files is for.
func TestGRIBIdentitiesAreUnique(t *testing.T) {
	t.Parallel()

	type identity struct {
		discipline, category, number, levelType uint8
		levelValue                              float64
	}

	seen := map[identity]field.Field{}

	for _, f := range field.Fields() {
		spec, err := field.SpecFor(f)
		if err != nil {
			t.Fatalf("SpecFor(%q): %v", f, err)
		}

		key := identity{
			spec.Discipline, spec.Category, spec.Number,
			spec.LevelType, spec.LevelValue,
		}

		if other, clash := seen[key]; clash {
			t.Errorf("%q and %q share the GRIB2 identity %+v; one of them would "+
				"never be populated", f, other, key)
		}

		seen[key] = f
	}
}

// Fields() drives the ingest field list. A set iterated in map order would
// fetch byte ranges in a different sequence every run, which makes a partial
// ingest impossible to compare against the one before it.
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

// Callers hold the slice; a shared backing array would let one of them
// reorder the vocabulary for everybody.
func TestFieldsReturnsAFreshSlice(t *testing.T) {
	t.Parallel()

	got := field.Fields()
	got[0] = "clobbered"

	if field.Fields()[0] == "clobbered" {
		t.Fatal("Fields() shares its backing array with its callers")
	}
}
