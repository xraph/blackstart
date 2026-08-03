package weather_test

import (
	"errors"
	"testing"
	"time"

	dsid "github.com/xraph/blackstart/extensions/datasource/id"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/weather"
	"github.com/xraph/blackstart/extensions/weather/field"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	wid "github.com/xraph/blackstart/extensions/weather/id"
	"github.com/xraph/blackstart/extensions/weather/store"
	"github.com/xraph/blackstart/extensions/weather/store/memory"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

var issuance = time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

func newEngine(t *testing.T) (*weather.Engine, store.Store) {
	t.Helper()

	s := memory.New()

	engine, err := weather.NewEngine(s, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	return engine, s
}

func aCycle() forecast.Cycle {
	return forecast.Cycle{
		ID:       wid.NewCycle(),
		FeedID:   dsid.NewFeed(),
		Model:    forecast.ModelHRRR,
		IssuedAt: issuance,
		Status:   forecast.StatusPending,
	}
}

func TestNewEngineRefusesNoStore(t *testing.T) {
	t.Parallel()

	if _, err := weather.NewEngine(nil, nil); err == nil {
		t.Fatal("NewEngine accepted a nil store")
	}
}

// Bounded backfill re-walks recent issuances on every worker restart, so
// recording a cycle that already exists is the common case rather than the
// exceptional one. Making it an error would have every restart log failures
// for working correctly.
func TestRecordCycleIsIdempotent(t *testing.T) {
	t.Parallel()

	engine, _ := newEngine(t)

	first, err := engine.RecordCycle(t.Context(), aCycle())
	if err != nil {
		t.Fatalf("first RecordCycle: %v", err)
	}

	// A different identifier, the same model and issuance — which is exactly
	// what a second backfill pass constructs.
	second, err := engine.RecordCycle(t.Context(), aCycle())
	if err != nil {
		t.Fatalf("second RecordCycle: %v", err)
	}

	if second.ID != first.ID {
		t.Errorf("the repeat minted a new cycle %s, want the existing %s",
			second.ID, first.ID)
	}

	got, err := engine.Cycles(t.Context(), store.CycleFilter{}, pkgstore.Page{})
	if err != nil {
		t.Fatalf("Cycles: %v", err)
	}

	if len(got) != 1 {
		t.Errorf("the store holds %d cycles, want 1", len(got))
	}
}

func TestCycleAtFindsByNaturalKey(t *testing.T) {
	t.Parallel()

	engine, _ := newEngine(t)

	want, err := engine.RecordCycle(t.Context(), aCycle())
	if err != nil {
		t.Fatalf("RecordCycle: %v", err)
	}

	got, err := engine.CycleAt(t.Context(), forecast.ModelHRRR, issuance)
	if err != nil {
		t.Fatalf("CycleAt: %v", err)
	}

	if got.ID != want.ID {
		t.Errorf("CycleAt returned %s, want %s", got.ID, want.ID)
	}

	if _, err := engine.CycleAt(
		t.Context(), forecast.ModelGFS, issuance,
	); !errors.Is(err, bserr.ErrNotFound) {
		t.Errorf("CycleAt(GFS) error = %v, want ErrNotFound", err)
	}
}

func TestSpanForecastReadsBackWhatWasWritten(t *testing.T) {
	t.Parallel()

	engine, s := newEngine(t)
	span := gid.NewSpan()

	if err := s.Samples().WriteForecast(t.Context(), []forecast.Sample{
		{
			SpanID: span, Field: field.Gust10m,
			IssuedAt: issuance, ValidAt: issuance.Add(time.Hour), Value: 14.2,
		},
		{
			SpanID: span, Field: field.Temp2m,
			IssuedAt: issuance, ValidAt: issuance.Add(time.Hour), Value: 305.4,
		},
	}); err != nil {
		t.Fatalf("WriteForecast: %v", err)
	}

	got, err := engine.SpanForecast(
		t.Context(), span, []field.Field{field.Gust10m},
		issuance, issuance, issuance.Add(6*time.Hour))
	if err != nil {
		t.Fatalf("SpanForecast: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("SpanForecast returned %d samples, want just the gust", len(got))
	}

	if got[0].Value != 14.2 {
		t.Errorf("value = %v, want 14.2", got[0].Value)
	}
}

// Asking for no fields means asking for all of them, which is what a caller
// inspecting a span actually wants.
func TestSpanForecastDefaultsToEveryField(t *testing.T) {
	t.Parallel()

	engine, s := newEngine(t)
	span := gid.NewSpan()

	if err := s.Samples().WriteForecast(t.Context(), []forecast.Sample{
		{
			SpanID: span, Field: field.Gust10m,
			IssuedAt: issuance, ValidAt: issuance.Add(time.Hour), Value: 14.2,
		},
		{
			SpanID: span, Field: field.Temp2m,
			IssuedAt: issuance, ValidAt: issuance.Add(time.Hour), Value: 305.4,
		},
	}); err != nil {
		t.Fatalf("WriteForecast: %v", err)
	}

	got, err := engine.SpanForecast(
		t.Context(), span, nil, issuance, issuance, issuance.Add(6*time.Hour))
	if err != nil {
		t.Fatalf("SpanForecast: %v", err)
	}

	if len(got) != 2 {
		t.Errorf("SpanForecast with no fields returned %d samples, want both", len(got))
	}
}

// Issuance is a parameter rather than inferred from the window, because "what
// does the latest run say" and "what did the 06Z run say" are different
// questions — and a reader who cannot ask the second cannot check a forecast
// against what happened.
func TestSpanForecastSelectsByIssuance(t *testing.T) {
	t.Parallel()

	engine, s := newEngine(t)
	span := gid.NewSpan()
	later := issuance.Add(6 * time.Hour)
	valid := issuance.Add(12 * time.Hour)

	if err := s.Samples().WriteForecast(t.Context(), []forecast.Sample{
		{SpanID: span, Field: field.Gust10m, IssuedAt: issuance, ValidAt: valid, Value: 14.2},
		{SpanID: span, Field: field.Gust10m, IssuedAt: later, ValidAt: valid, Value: 21.0},
	}); err != nil {
		t.Fatalf("WriteForecast: %v", err)
	}

	got, err := engine.SpanForecast(
		t.Context(), span, []field.Field{field.Gust10m},
		later, issuance, valid.Add(time.Hour))
	if err != nil {
		t.Fatalf("SpanForecast: %v", err)
	}

	if len(got) != 1 || got[0].Value != 21.0 {
		t.Fatalf("got %+v, want only the later issuance's 21.0", got)
	}
}

func TestSpanForecastRejectsBadInput(t *testing.T) {
	t.Parallel()

	engine, _ := newEngine(t)

	if _, err := engine.SpanForecast(
		t.Context(), gid.NewSpan(), []field.Field{"gust_10m"},
		issuance, issuance, issuance.Add(time.Hour),
	); err == nil {
		t.Error("SpanForecast accepted a field outside the vocabulary")
	}

	if _, err := engine.SpanForecast(
		t.Context(), id.Nil, nil, issuance, issuance, issuance.Add(time.Hour),
	); err == nil {
		t.Error("SpanForecast accepted a zero span")
	}
}

func TestGridsForCycle(t *testing.T) {
	t.Parallel()

	engine, s := newEngine(t)

	cycle, err := engine.RecordCycle(t.Context(), aCycle())
	if err != nil {
		t.Fatalf("RecordCycle: %v", err)
	}

	if err := s.Grids().Create(t.Context(), forecast.Grid{
		ID:               wid.NewGrid(),
		CycleID:          cycle.ID,
		ForecastHour:     1,
		Field:            field.Gust10m,
		BlobRef:          "sha256:abc",
		DatasetVersionID: dsid.NewFeed(),
	}); err != nil {
		t.Fatalf("Create grid: %v", err)
	}

	got, err := engine.Grids(t.Context(), cycle.ID)
	if err != nil {
		t.Fatalf("Grids: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("Grids returned %d, want 1", len(got))
	}

	// Every retrieval names its dataset version. Without it a sample derived
	// from this grid could not be traced to the bytes it came from, which is
	// the promise ADR 0006 makes.
	if got[0].DatasetVersionID.IsZero() {
		t.Error("the stored grid has no dataset version")
	}
}
