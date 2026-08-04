package weather_test

import (
	"context"
	"errors"
	"testing"
	"time"

	geoasset "github.com/xraph/blackstart/extensions/geo/asset"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/geo/shape"
	pvid "github.com/xraph/blackstart/extensions/provenance/id"
	"github.com/xraph/blackstart/extensions/weather"
	"github.com/xraph/blackstart/extensions/weather/field"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	wid "github.com/xraph/blackstart/extensions/weather/id"
	"github.com/xraph/blackstart/extensions/weather/store"
	"github.com/xraph/blackstart/extensions/weather/store/memory"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/solver"
)

// fakeGeometry serves a fixed set of routes and spans.
type fakeGeometry struct {
	routes []geoasset.Route
	spans  map[id.ID][]geoasset.Span
	err    error
}

func (g *fakeGeometry) Routes(context.Context, int) ([]geoasset.Route, error) {
	return g.routes, g.err
}

func (g *fakeGeometry) Spans(_ context.Context, route id.ID) ([]geoasset.Span, error) {
	return g.spans[route], g.err
}

// fakeGrids records what it was asked and answers with a fixed pattern.
type fakeGrids struct {
	// absentAt marks point indexes that fall outside the domain.
	absentAt map[int]bool

	// value is what every present point returns.
	value float64

	// short makes SampleGrid return one fewer sample than asked.
	short bool

	uploadErr error

	uploads int
	asked   [][]solver.GridPoint
}

func (g *fakeGrids) UploadGrid(
	context.Context, []byte,
) (string, solver.GridDescription, error) {
	g.uploads++

	if g.uploadErr != nil {
		return "", solver.GridDescription{}, g.uploadErr
	}

	return "sha256:fake", solver.GridDescription{Projection: "lambert"}, nil
}

func (g *fakeGrids) SampleGrid(
	_ context.Context, _ string, points []solver.GridPoint,
) ([]solver.GridSample, error) {
	g.asked = append(g.asked, points)

	count := len(points)
	if g.short {
		count--
	}

	out := make([]solver.GridSample, 0, count)

	for index := range count {
		if g.absentAt[index] {
			out = append(out, solver.GridSample{Present: false})

			continue
		}

		out = append(out, solver.GridSample{Present: true, Value: g.value})
	}

	return out, nil
}

// aSpan builds a span whose geometry has a known midpoint.
func aSpan(lon, lat float64) geoasset.Span {
	return geoasset.Span{
		ID:      gid.NewSpan(),
		RouteID: gid.NewRoute(),
		Geometry: shape.Line{
			{Lon: lon - 0.01, Lat: lat},
			{Lon: lon, Lat: lat},
			{Lon: lon + 0.01, Lat: lat},
		},
	}
}

func sampleHarness(
	t *testing.T, grids *fakeGrids, spans []geoasset.Span,
) (*weather.Engine, store.Store, id.ID) {
	t.Helper()

	s := memory.New()
	route := gid.NewRoute()

	geometry := &fakeGeometry{
		routes: []geoasset.Route{{ID: route}},
		spans:  map[id.ID][]geoasset.Span{route: spans},
	}

	engine, err := weather.NewEngine(s, nil,
		weather.WithSampling(geometry, grids),
		weather.WithClock(func() time.Time { return issuance }))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	cycle := forecast.Cycle{
		ID:       wid.NewCycle(),
		FeedID:   pvid.NewDataset(),
		Model:    forecast.ModelHRRR,
		IssuedAt: issuance,
		Status:   forecast.StatusFetched,
	}

	if err := s.Cycles().Create(t.Context(), cycle); err != nil {
		t.Fatalf("Create cycle: %v", err)
	}

	reference, err := s.Blobs().Put(t.Context(), []byte("grib bytes"))
	if err != nil {
		t.Fatalf("Put blob: %v", err)
	}

	if err := s.Grids().Create(t.Context(), forecast.Grid{
		ID:               wid.NewGrid(),
		CycleID:          cycle.ID,
		ForecastHour:     6,
		Field:            field.Gust10m,
		BlobRef:          reference,
		DatasetVersionID: pvid.NewDataset(),
	}); err != nil {
		t.Fatalf("Create grid: %v", err)
	}

	return engine, s, cycle.ID
}

func TestSampleCycleWritesSamplesUnderTheRightKey(t *testing.T) {
	t.Parallel()

	spans := []geoasset.Span{aSpan(-97.67, 30.19), aSpan(-95.37, 29.76)}
	grids := &fakeGrids{value: 14.2}

	engine, s, cycleID := sampleHarness(t, grids, spans)

	got, err := engine.SampleCycle(t.Context(), cycleID, nil)
	if err != nil {
		t.Fatalf("SampleCycle: %v", err)
	}

	if got.Samples != 2 {
		t.Errorf("wrote %d samples, want 2", got.Samples)
	}

	// The valid time is the issuance plus the forecast hour, and the key
	// carries the issuance — so reading back under exactly that key is what
	// proves the two axes stayed apart.
	validAt := issuance.Add(6 * time.Hour)

	key := forecast.SampleKey(spans[0].ID, field.Gust10m, issuance)

	back, err := s.Samples().ReadForecast(
		t.Context(), []string{key}, validAt, validAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("ReadForecast: %v", err)
	}

	if len(back) != 1 {
		t.Fatalf("read back %d samples, want 1", len(back))
	}

	if back[0].Value != 14.2 {
		t.Errorf("value = %v, want 14.2", back[0].Value)
	}

	if !back[0].ValidAt.Equal(validAt) {
		t.Errorf("valid at %s, want %s", back[0].ValidAt, validAt)
	}
}

// The whole reason Present exists. A span outside CONUS contributes nothing,
// and the covered count is where that stays visible.
func TestAnAbsentPointStoresNothingAndLowersCovered(t *testing.T) {
	t.Parallel()

	spans := []geoasset.Span{
		aSpan(-97.67, 30.19),
		aSpan(0, 0), // outside the domain
		aSpan(-95.37, 29.76),
	}

	grids := &fakeGrids{value: 14.2, absentAt: map[int]bool{1: true}}

	engine, s, cycleID := sampleHarness(t, grids, spans)

	got, err := engine.SampleCycle(t.Context(), cycleID, nil)
	if err != nil {
		t.Fatalf("SampleCycle: %v", err)
	}

	if got.Samples != 2 {
		t.Errorf("wrote %d samples, want 2 of 3 spans", got.Samples)
	}

	if got.Covered != 2 || got.Total != 3 {
		t.Errorf("covered/total = %d/%d, want 2/3", got.Covered, got.Total)
	}

	// The absent span must have no series at all — not a zero.
	key := forecast.SampleKey(spans[1].ID, field.Gust10m, issuance)

	back, err := s.Samples().ReadForecast(
		t.Context(), []string{key}, issuance, issuance.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("ReadForecast: %v", err)
	}

	if len(back) != 0 {
		t.Errorf("the absent span stored %d samples; a zero here is a "+
			"fabricated calm", len(back))
	}
}

func TestMarkSampledRecordsBothCounts(t *testing.T) {
	t.Parallel()

	spans := []geoasset.Span{aSpan(-97.67, 30.19), aSpan(0, 0)}
	grids := &fakeGrids{value: 5, absentAt: map[int]bool{1: true}}

	engine, s, cycleID := sampleHarness(t, grids, spans)

	if _, err := engine.SampleCycle(t.Context(), cycleID, nil); err != nil {
		t.Fatalf("SampleCycle: %v", err)
	}

	stored, err := s.Grids().ForCycle(t.Context(), cycleID)
	if err != nil {
		t.Fatalf("ForCycle: %v", err)
	}

	if stored[0].Covered != 1 || stored[0].Total != 2 {
		t.Errorf("grid covered/total = %d/%d, want 1/2",
			stored[0].Covered, stored[0].Total)
	}

	if stored[0].SampledAt.IsZero() {
		t.Error("the grid was not marked sampled")
	}
}

func TestSampleCycleAdvancesTheStatus(t *testing.T) {
	t.Parallel()

	grids := &fakeGrids{value: 1}
	engine, s, cycleID := sampleHarness(t, grids, []geoasset.Span{aSpan(-97.67, 30.19)})

	if _, err := engine.SampleCycle(t.Context(), cycleID, nil); err != nil {
		t.Fatalf("SampleCycle: %v", err)
	}

	cycle, err := s.Cycles().Get(t.Context(), cycleID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if cycle.Status != forecast.StatusSampled {
		t.Errorf("status = %q, want sampled", cycle.Status)
	}
}

// The grid goes up once and is sampled for every span in one call. Uploading
// per span would re-decode two million points per span.
func TestAGridIsUploadedOncePerPass(t *testing.T) {
	t.Parallel()

	spans := []geoasset.Span{
		aSpan(-97.67, 30.19), aSpan(-95.37, 29.76), aSpan(-96.80, 32.78),
	}

	grids := &fakeGrids{value: 1}
	engine, _, cycleID := sampleHarness(t, grids, spans)

	if _, err := engine.SampleCycle(t.Context(), cycleID, nil); err != nil {
		t.Fatalf("SampleCycle: %v", err)
	}

	if grids.uploads != 1 {
		t.Errorf("uploaded %d times for one grid, want 1", grids.uploads)
	}

	if len(grids.asked) != 1 {
		t.Fatalf("sampled in %d calls, want 1 batch", len(grids.asked))
	}

	if len(grids.asked[0]) != 3 {
		t.Errorf("asked for %d points, want all 3 spans in one call",
			len(grids.asked[0]))
	}
}

// Results pair with requests by position, so a short response would attribute
// weather to the wrong spans — which must fail rather than store.
func TestAShortSampleResponseIsRefused(t *testing.T) {
	t.Parallel()

	spans := []geoasset.Span{aSpan(-97.67, 30.19), aSpan(-95.37, 29.76)}
	grids := &fakeGrids{value: 1, short: true}

	engine, s, cycleID := sampleHarness(t, grids, spans)

	if _, err := engine.SampleCycle(t.Context(), cycleID, nil); err == nil {
		t.Fatal("a short sample response was accepted")
	}

	stored, err := s.Grids().ForCycle(t.Context(), cycleID)
	if err != nil {
		t.Fatalf("ForCycle: %v", err)
	}

	if !stored[0].SampledAt.IsZero() {
		t.Error("a grid was marked sampled after a refused response")
	}
}

func TestSampleCycleNeedsSpans(t *testing.T) {
	t.Parallel()

	grids := &fakeGrids{value: 1}
	engine, _, cycleID := sampleHarness(t, grids, nil)

	if _, err := engine.SampleCycle(t.Context(), cycleID, nil); err == nil {
		t.Fatal("sampling with no spans succeeded")
	}
}

func TestAReadOnlyEngineRefusesToSample(t *testing.T) {
	t.Parallel()

	engine, err := weather.NewEngine(memory.New(), nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	if _, err := engine.SampleCycle(t.Context(), wid.NewCycle(), nil); err == nil {
		t.Fatal("a read-only engine sampled")
	}
}

func TestAnUploadFailureStopsThePass(t *testing.T) {
	t.Parallel()

	grids := &fakeGrids{value: 1, uploadErr: errors.New("service down")}
	engine, _, cycleID := sampleHarness(t, grids, []geoasset.Span{aSpan(-97.67, 30.19)})

	if _, err := engine.SampleCycle(t.Context(), cycleID, nil); err == nil {
		t.Fatal("a failed upload was tolerated")
	}
}
