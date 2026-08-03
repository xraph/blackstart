package forecast_test

import (
	"errors"
	"testing"
	"time"

	dsid "github.com/xraph/blackstart/extensions/datasource/id"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	pvid "github.com/xraph/blackstart/extensions/provenance/id"
	"github.com/xraph/blackstart/extensions/weather/field"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	wid "github.com/xraph/blackstart/extensions/weather/id"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// The series key is the only place issuance survives, because the Timeseries
// port carries one string key and one float per point with no column for it.
// A key written one way and composed another way for reading produces an
// empty series rather than an error, so the round trip is asserted.
func TestSampleKeyRoundTrips(t *testing.T) {
	t.Parallel()

	span := gid.NewSpan()
	issued := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

	key := forecast.SampleKey(span, field.Gust10m, issued)

	gotSpan, gotField, gotIssued, err := forecast.ParseSampleKey(key)
	if err != nil {
		t.Fatalf("ParseSampleKey(%q): %v", key, err)
	}

	if gotSpan != span {
		t.Errorf("span = %s, want %s", gotSpan, span)
	}

	if gotField != field.Gust10m {
		t.Errorf("field = %q, want %q", gotField, field.Gust10m)
	}

	if !gotIssued.Equal(issued) {
		t.Errorf("issuance = %s, want %s", gotIssued, issued)
	}
}

// Every field must survive the round trip, not just the one that happened to
// be written first.
func TestSampleKeyRoundTripsEveryField(t *testing.T) {
	t.Parallel()

	span := gid.NewSpan()
	issued := time.Date(2026, 2, 14, 6, 0, 0, 0, time.UTC)

	for _, f := range field.Fields() {
		key := forecast.SampleKey(span, f, issued)

		_, got, _, err := forecast.ParseSampleKey(key)
		if err != nil {
			t.Errorf("ParseSampleKey(%q): %v", key, err)

			continue
		}

		if got != f {
			t.Errorf("field = %q, want %q", got, f)
		}
	}
}

// Issuance is stored in UTC. The same instant written from two zones must
// produce one key, or one forecast lands in two series and each looks half
// empty.
func TestSampleKeyNormalizesToUTC(t *testing.T) {
	t.Parallel()

	span := gid.NewSpan()
	utc := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	elsewhere := utc.In(time.FixedZone("test", 5*60*60))

	if forecast.SampleKey(span, field.Gust10m, utc) !=
		forecast.SampleKey(span, field.Gust10m, elsewhere) {
		t.Fatal("the same instant in two zones produced two keys")
	}
}

func TestParseSampleKeyRejectsMalformed(t *testing.T) {
	t.Parallel()

	span := gid.NewSpan().String()

	for name, key := range map[string]string{
		"empty":            "",
		"no delimiters":    "nothing-here",
		"two parts":        span + "|gust10m",
		"four parts":       span + "|gust10m|20260803T1200Z|extra",
		"unknown field":    span + "|gust_10m|20260803T1200Z",
		"not a span":       wid.NewGrid().String() + "|gust10m|20260803T1200Z",
		"bad issuance":     span + "|gust10m|yesterday",
		"rfc3339 issuance": span + "|gust10m|2026-08-03T12:00:00Z",
	} {
		if _, _, _, err := forecast.ParseSampleKey(key); err == nil {
			t.Errorf("%s: ParseSampleKey(%q) accepted a malformed key", name, key)
		}
	}
}

func TestCycleValidation(t *testing.T) {
	t.Parallel()

	valid := forecast.Cycle{
		ID:       wid.NewCycle(),
		FeedID:   dsid.NewFeed(),
		Model:    forecast.ModelHRRR,
		IssuedAt: time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC),
		Status:   forecast.StatusPending,
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("a well-formed cycle was rejected: %v", err)
	}

	for name, mutate := range map[string]func(*forecast.Cycle){
		"no feed":     func(c *forecast.Cycle) { c.FeedID = id.Nil },
		"no model":    func(c *forecast.Cycle) { c.Model = "" },
		"bad model":   func(c *forecast.Cycle) { c.Model = "ecmwf" },
		"no issuance": func(c *forecast.Cycle) { c.IssuedAt = time.Time{} },
		"bad status":  func(c *forecast.Cycle) { c.Status = "halfway" },
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

func TestGridValidation(t *testing.T) {
	t.Parallel()

	valid := forecast.Grid{
		ID:               wid.NewGrid(),
		CycleID:          wid.NewCycle(),
		ForecastHour:     6,
		Field:            field.Gust10m,
		BlobRef:          "blob_01",
		DatasetVersionID: pvid.NewDataset(),
		NX:               1799,
		NY:               1059,
		Projection:       "lambert",
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("a well-formed grid was rejected: %v", err)
	}

	for name, mutate := range map[string]func(*forecast.Grid){
		"no cycle":      func(g *forecast.Grid) { g.CycleID = id.Nil },
		"unknown field": func(g *forecast.Grid) { g.Field = "gust_10m" },
		"negative hour": func(g *forecast.Grid) { g.ForecastHour = -1 },
		"no blob":       func(g *forecast.Grid) { g.BlobRef = "" },
		"no dataset":    func(g *forecast.Grid) { g.DatasetVersionID = id.Nil },
	} {
		broken := valid
		mutate(&broken)

		if err := broken.Validate(); err == nil {
			t.Errorf("%s: validated", name)
		}
	}
}

// A forecast hour is hours after issuance, so the valid time it describes is
// derived rather than stored twice — two columns that must agree are two
// columns that eventually will not.
func TestGridValidAt(t *testing.T) {
	t.Parallel()

	issued := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	grid := forecast.Grid{ForecastHour: 18}

	want := time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC)

	if got := grid.ValidAt(issued); !got.Equal(want) {
		t.Errorf("ValidAt = %s, want %s", got, want)
	}
}

// A forecast cannot describe a moment that preceded the run that produced it.
// Catching that here means a sampling bug surfaces as a rejected write rather
// than as a plausible number in a training set.
func TestSampleRejectsValidTimeBeforeIssuance(t *testing.T) {
	t.Parallel()

	issued := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

	sample := forecast.Sample{
		SpanID:   gid.NewSpan(),
		Field:    field.Gust10m,
		IssuedAt: issued,
		ValidAt:  issued.Add(-time.Hour),
		Value:    14.2,
	}

	err := sample.Validate()
	if err == nil {
		t.Fatal("a sample valid before its own issuance was accepted")
	}

	if !errors.Is(err, bserr.ErrInvalid) {
		t.Fatalf("error = %v, want bserr.ErrInvalid", err)
	}
}

// The analysis hour is valid exactly at issuance, and must not be rejected by
// the check above.
func TestSampleAcceptsTheAnalysisHour(t *testing.T) {
	t.Parallel()

	issued := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

	sample := forecast.Sample{
		SpanID:   gid.NewSpan(),
		Field:    field.Gust10m,
		IssuedAt: issued,
		ValidAt:  issued,
		Value:    14.2,
	}

	if err := sample.Validate(); err != nil {
		t.Fatalf("the analysis hour was rejected: %v", err)
	}
}

func TestModelSetIsClosed(t *testing.T) {
	t.Parallel()

	for _, m := range forecast.Models() {
		if !m.Valid() {
			t.Errorf("%q is in Models() but reports invalid", m)
		}
	}

	if forecast.Model("ecmwf").Valid() {
		t.Error("a model outside the registry reports valid")
	}
}
