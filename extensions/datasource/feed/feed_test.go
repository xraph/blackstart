package feed

import (
	"errors"
	"strings"
	"testing"

	dsid "github.com/xraph/blackstart/extensions/datasource/id"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

func valid() Feed {
	return Feed{
		ID:       dsid.NewFeed(),
		SourceID: dsid.NewSource(),
		Slug:     "hrrr",
		Name:     "High-Resolution Rapid Refresh",
		URL:      "https://noaa-hrrr-bdp-pds.s3.amazonaws.com",
		Format:   FormatGRIB2,
		Cadence:  "hourly",
	}
}

func TestValidAccepted(t *testing.T) {
	t.Parallel()

	if err := valid().Validate(); err != nil {
		t.Errorf("a well-formed feed was rejected: %v", err)
	}
}

// The scheme check is a safety property, not tidiness. An unattended worker
// fetches these URLs, so a file:// entry would have it reading the local
// filesystem on the strength of a database row, and an ftp:// entry would open
// a plaintext connection.
func TestOnlyHTTPSchemesAreAccepted(t *testing.T) {
	t.Parallel()

	dangerous := []string{
		"file:///etc/passwd",
		"file://localhost/etc/shadow",
		"ftp://ftp.example.gov/data.csv",
		"gopher://example.gov",
		"data:text/plain;base64,aGVsbG8=",
		"javascript:alert(1)",
	}

	for _, raw := range dangerous {
		f := valid()
		f.URL = raw

		err := f.Validate()
		if err == nil {
			t.Errorf("accepted the URL %q; an unattended worker fetches these", raw)

			continue
		}

		if !errors.Is(err, bserr.ErrInvalid) {
			t.Errorf("rejecting %q should carry the invalid kind: %v", raw, err)
		}
	}

	for _, raw := range []string{
		"http://example.gov/data.csv",
		"https://example.gov/data.csv",
		"https://noaa-hrrr-bdp-pds.s3.amazonaws.com/hrrr.{date}/conus/",
	} {
		f := valid()
		f.URL = raw

		if err := f.Validate(); err != nil {
			t.Errorf("URL %q should be accepted: %v", raw, err)
		}
	}
}

func TestURLMustHaveAHost(t *testing.T) {
	t.Parallel()

	f := valid()
	f.URL = "https:///no-host/path"

	if err := f.Validate(); err == nil {
		t.Error("accepted a URL with no host")
	}
}

// A feed must belong to a source, because that field is what the graph
// projection follows. A feed without one is invisible in the graph.
func TestFeedMustBelongToASource(t *testing.T) {
	t.Parallel()

	f := valid()
	f.SourceID = id.Nil

	err := f.Validate()
	if err == nil {
		t.Fatal("accepted a feed with no source")
	}

	if !strings.Contains(err.Error(), "graph projection") {
		t.Errorf("the error should say why the field matters: %v", err)
	}
}

// The identifier types must not be interchangeable — a source identifier in
// the feed's own ID field, or a feed identifier as its parent.
func TestIdentifierTypesAreNotInterchangeable(t *testing.T) {
	t.Parallel()

	wrongOwnID := valid()
	wrongOwnID.ID = dsid.NewSource()

	if err := wrongOwnID.Validate(); err == nil {
		t.Error("accepted a source identifier as the feed's own identifier")
	}

	wrongParent := valid()
	wrongParent.SourceID = dsid.NewFeed()

	if err := wrongParent.Validate(); err == nil {
		t.Error("accepted a feed identifier as the parent source")
	}
}

func TestFormatIsAClosedSet(t *testing.T) {
	t.Parallel()

	for _, f := range Formats() {
		if !f.Valid() {
			t.Errorf("%q is listed by Formats() but reports itself invalid", f)
		}
	}

	for _, f := range []Format{"", "netcdf", "GRIB2", "parquet"} {
		if f.Valid() {
			t.Errorf("%q should not be a valid format", f)
		}
	}
}

func TestValidationFailuresAreInvalidKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Feed)
	}{
		{"no identifier", func(f *Feed) { f.ID = id.Nil }},
		{"no source", func(f *Feed) { f.SourceID = id.Nil }},
		{"no slug", func(f *Feed) { f.Slug = " " }},
		{"unknown format", func(f *Feed) { f.Format = "netcdf" }},
		{"no URL", func(f *Feed) { f.URL = "" }},
		{"malformed URL", func(f *Feed) { f.URL = "ht tp://bad" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := valid()
			tt.mutate(&f)

			err := f.Validate()
			if err == nil {
				t.Fatal("accepted an invalid feed")
			}

			if !errors.Is(err, bserr.ErrInvalid) {
				t.Errorf("error should carry the invalid kind: %v", err)
			}
		})
	}
}

func TestSentinelsCarryTheirSharedKind(t *testing.T) {
	t.Parallel()

	if !errors.Is(ErrNotFound, bserr.ErrNotFound) {
		t.Error("ErrNotFound does not wrap the shared not-found kind")
	}

	if !errors.Is(ErrSlugTaken, bserr.ErrConflict) {
		t.Error("ErrSlugTaken does not wrap the shared conflict kind")
	}
}
