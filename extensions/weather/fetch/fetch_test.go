package fetch_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/xraph/blackstart/extensions/weather/fetch"
)

// The range header is the one thing here that has to be exactly right. Off by
// one produces a message short by a byte, which fails to decode with an error
// a long way from its cause.
func TestRangeAsksForAnInclusiveRange(t *testing.T) {
	t.Parallel()

	var asked string

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			asked = r.Header.Get("Range")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(make([]byte, 100))
		}))

	t.Cleanup(server.Close)

	client := fetch.New(server.Client())

	if _, err := client.Range(t.Context(), server.URL, 3231216, 100); err != nil {
		t.Fatalf("Range: %v", err)
	}

	// Inclusive at both ends: 100 bytes from 3231216 ends at 3231315.
	if want := "bytes=3231216-3231315"; asked != want {
		t.Errorf("Range header = %q, want %q", asked, want)
	}
}

// A length of zero is the last record in a sidecar: there is no next offset,
// so the message runs to the end of the file.
func TestAZeroLengthAsksForAnOpenEndedRange(t *testing.T) {
	t.Parallel()

	var asked string

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			asked = r.Header.Get("Range")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("tail"))
		}))

	t.Cleanup(server.Close)

	client := fetch.New(server.Client())

	if _, err := client.Range(t.Context(), server.URL, 500, 0); err != nil {
		t.Fatalf("Range: %v", err)
	}

	if want := "bytes=500-"; asked != want {
		t.Errorf("Range header = %q, want %q", asked, want)
	}
}

// A server that ignored the range sends the whole product. Those bytes would
// decode as the file's first message — a different field entirely — and
// nothing downstream would notice.
func TestAShortOrLongResponseIsRefused(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(make([]byte, 40))
		}))

	t.Cleanup(server.Close)

	client := fetch.New(server.Client())

	if _, err := client.Range(t.Context(), server.URL, 0, 100); err == nil {
		t.Error("a response of the wrong length was accepted")
	}
}

// A cycle NOAA has not posted yet is the ordinary state of the most recent
// hour. A worker treating it as an error would log a failure every tick for
// working correctly.
func TestAnAbsentProductIsNotPublishedRatherThanAnError(t *testing.T) {
	t.Parallel()

	for name, code := range map[string]int{
		"not found": http.StatusNotFound,
		// S3 answers a missing key with 403 when listing is denied, which is
		// how an unpublished cycle presents on the public buckets.
		"forbidden": http.StatusForbidden,
	} {
		server := httptest.NewServer(http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(code)
			}))

		client := fetch.New(server.Client())

		_, err := client.Index(t.Context(), server.URL)
		if !errors.Is(err, fetch.ErrNotPublished) {
			t.Errorf("%s: error = %v, want ErrNotPublished", name, err)
		}

		server.Close()
	}
}

func TestAServerErrorIsAnError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))

	t.Cleanup(server.Close)

	client := fetch.New(server.Client())

	_, err := client.Index(t.Context(), server.URL)
	if err == nil {
		t.Fatal("a 500 was accepted")
	}

	if errors.Is(err, fetch.ErrNotPublished) {
		t.Error("a 500 was reported as not-yet-published; a worker would retry forever")
	}
}

// End to end over HTTP: serve the real sidecar, parse it, and locate a field.
func TestIndexOverHTTP(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile(filepath.Join("testdata", "hrrr.t12z.wrfsfcf01.grib2.idx"))
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(body)
		}))

	t.Cleanup(server.Close)

	records, err := fetch.New(server.Client()).Index(t.Context(), server.URL)
	if err != nil {
		t.Fatalf("Index: %v", err)
	}

	offset, length, ok := records.Locate("GUST:surface")
	if !ok {
		t.Fatal("GUST:surface not located")
	}

	if offset != 3231216 || length != 4460131-3231216 {
		t.Errorf("located %d+%d", offset, length)
	}
}

func TestARangeWithNegativeBoundsIsRefused(t *testing.T) {
	t.Parallel()

	client := fetch.New(nil)

	if _, err := client.Range(t.Context(), "http://example.invalid", -1, 10); err == nil {
		t.Error("a negative offset was accepted")
	}

	if _, err := client.Range(t.Context(), "http://example.invalid", 0, -10); err == nil {
		t.Error("a negative length was accepted")
	}
}

// An unattended worker follows these URLs. A feed row pointing at the cloud
// metadata endpoint would have it fetch instance credentials and store them in
// the byte plane as though they were weather.
func TestLinkLocalAddressesAreRefused(t *testing.T) {
	t.Parallel()

	client := fetch.New(nil)

	for _, raw := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://[fe80::1]/",
	} {
		if _, err := client.Index(t.Context(), raw); err == nil {
			t.Errorf("%s was fetched", raw)
		} else if errors.Is(err, fetch.ErrNotPublished) {
			t.Errorf("%s was reported as merely unpublished", raw)
		}
	}
}

func TestNonHTTPSchemesAreRefused(t *testing.T) {
	t.Parallel()

	client := fetch.New(nil)

	for _, raw := range []string{
		"file:///etc/passwd",
		"ftp://example.gov/hrrr",
		"gopher://example.gov/",
	} {
		if _, err := client.Index(t.Context(), raw); err == nil {
			t.Errorf("%s was fetched", raw)
		}
	}
}
