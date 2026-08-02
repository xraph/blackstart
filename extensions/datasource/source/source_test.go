package source

import (
	"errors"
	"strings"
	"testing"

	dsid "github.com/xraph/blackstart/extensions/datasource/id"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

func valid() Source {
	return Source{
		ID:       dsid.NewSource(),
		Slug:     "tamu-activsg",
		Name:     "Texas A&M ACTIVSg synthetic grids",
		Kind:     KindNetwork,
		Provides: "Realistic synthetic networks free of CEII restriction",
		Homepage: "https://electricgrids.engr.tamu.edu",
		License:  "Free for research and educational use",
	}
}

func TestValidAccepted(t *testing.T) {
	t.Parallel()

	if err := valid().Validate(); err != nil {
		t.Errorf("a well-formed source was rejected: %v", err)
	}
}

// Every validation failure must carry the shared invalid kind, so transport
// maps it to 400 without importing this package.
func TestValidationFailuresAreInvalidKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Source)
	}{
		{"no identifier", func(s *Source) { s.ID = id.Nil }},
		{"wrong identifier type", func(s *Source) { s.ID = dsid.NewFeed() }},
		{"empty slug", func(s *Source) { s.Slug = "" }},
		{"slug with spaces", func(s *Source) { s.Slug = "tamu activsg" }},
		{"slug with uppercase", func(s *Source) { s.Slug = "TAMU" }},
		{"slug with underscore", func(s *Source) { s.Slug = "tamu_activsg" }},
		{"no name", func(s *Source) { s.Name = "  " }},
		{"unknown kind", func(s *Source) { s.Kind = "telemetry" }},
		{"empty kind", func(s *Source) { s.Kind = "" }},
		{"no license", func(s *Source) { s.License = "" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := valid()
			tt.mutate(&s)

			err := s.Validate()
			if err == nil {
				t.Fatal("accepted an invalid source")
			}

			if !errors.Is(err, bserr.ErrInvalid) {
				t.Errorf("error should carry the invalid kind so transport can "+
					"classify it without importing this package: %v", err)
			}
		})
	}
}

// A feed identifier reaching a source field is exactly the confusion
// type-prefixed identifiers exist to catch.
func TestValidateRejectsAnIdentifierOfTheWrongType(t *testing.T) {
	t.Parallel()

	s := valid()
	s.ID = dsid.NewFeed()

	err := s.Validate()
	if err == nil {
		t.Fatal("accepted a feed identifier as a source identifier")
	}

	if !strings.Contains(err.Error(), "dfeed") {
		t.Errorf("the error should show the identifier it got: %v", err)
	}
}

// The registry is a closed set. A free-text kind would let a source outside
// spec §8 arrive without anyone noticing, which is the constraint the whole
// public-data guarantee rests on.
func TestKindIsAClosedSet(t *testing.T) {
	t.Parallel()

	for _, k := range Kinds() {
		if !k.Valid() {
			t.Errorf("%q is listed by Kinds() but reports itself invalid", k)
		}
	}

	for _, k := range []Kind{"", "telemetry", "Network", "NETWORK", "other"} {
		if k.Valid() {
			t.Errorf("%q should not be a valid kind", k)
		}
	}
}

// The license field is required because the project's public-data guarantee
// depends on knowing the terms of every source. An unlicensed entry would let
// that guarantee quietly become false.
func TestLicenseIsRequired(t *testing.T) {
	t.Parallel()

	s := valid()
	s.License = ""

	err := s.Validate()
	if err == nil {
		t.Fatal("accepted a source with no license recorded")
	}

	if !strings.Contains(err.Error(), "public-data") {
		t.Errorf("the error should say why it matters: %v", err)
	}
}

func TestSlugsUsableOnTheCommandLine(t *testing.T) {
	t.Parallel()

	for _, slug := range []string{"noaa-hrrr", "eagle-i", "eia-860", "usgs3dep", "a"} {
		s := valid()
		s.Slug = slug

		if err := s.Validate(); err != nil {
			t.Errorf("slug %q should be accepted: %v", slug, err)
		}
	}

	for _, slug := range []string{"-leading", "trailing-", "double--hyphen", "has/slash"} {
		s := valid()
		s.Slug = slug

		if err := s.Validate(); err == nil {
			t.Errorf("slug %q should be rejected", slug)
		}
	}
}

func TestSentinelsCarryTheirSharedKind(t *testing.T) {
	t.Parallel()

	if !errors.Is(ErrNotFound, bserr.ErrNotFound) {
		t.Error("ErrNotFound does not wrap the shared not-found kind")
	}

	for _, err := range []error{ErrSlugTaken, ErrAlreadyExists} {
		if !errors.Is(err, bserr.ErrConflict) {
			t.Errorf("%v does not wrap the shared conflict kind", err)
		}
	}
}
