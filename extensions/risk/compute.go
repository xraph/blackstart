// Package risk turns weather-derived loading into per-component failure
// probabilities: a pass over a weather cycle that reads what hazard already
// computed — wind and ice loading per span — and multiplies it through a
// fragility curve to get a probability per conductor and per structure. It
// mirrors extensions/hazard/compute.go in shape: narrow interfaces onto the
// extensions it reads from, counters that keep distinct outcomes apart, and
// an optional provenance recorder.
//
// Nothing here guesses. A structure whose Kind maps to no curve is counted
// Unclassed and writes nothing; a span with no hazard values is counted
// Unhazarded, separately — one is a gap in the asset record, the other in the
// weather coverage, and only one of them is anybody's to fix.
package risk

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	geoasset "github.com/xraph/blackstart/extensions/geo/asset"
	hzstore "github.com/xraph/blackstart/extensions/hazard/store"
	"github.com/xraph/blackstart/extensions/provenance"
	"github.com/xraph/blackstart/extensions/provenance/record"
	"github.com/xraph/blackstart/extensions/risk/fragility"
	rid "github.com/xraph/blackstart/extensions/risk/id"
	"github.com/xraph/blackstart/extensions/risk/store"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	"github.com/xraph/blackstart/pkg/buildinfo"
	"github.com/xraph/blackstart/pkg/hooks"
	"github.com/xraph/blackstart/pkg/id"
)

// ExtensionName is how this extension is identified to Forge.
const ExtensionName = "risk"

// The producer identity. ModelVersion tracks output-changing changes to this
// package, not releases, for the reason hazard's own does not track those.
const (
	ModelName    = "bs-risk"
	ModelVersion = "0.1.0"
)

// ScalarUncalibrated is what multiplies every probability until calibration
// fits a real one. Recorded on every value, so a figure computed now and one
// computed after calibration can be told apart.
const ScalarUncalibrated = 1.0

// conductorHours is the duration ConductorProbability integrates over:
// QuantityConductorFailure is one row per hour ending at ValidAt, not a
// running total, so every call uses this rather than a gap between hours.
const conductorHours = 1.0

// everyRoute is "no limit" expressed as a large one (extensions/hazard's
// inputs.go uses the same idiom). widestHorizon bounds how far past a
// cycle's issuance this pass looks for hazard values already written; unlike
// hazard's own horizon it cannot trim to actual coverage (see [Weather]), so
// this costs one empty query on a span hazard never reached.
const (
	everyRoute    = 10000
	widestHorizon = 21 * 24 * time.Hour
)

// Hazard is the part of the hazard extension a computation reads. An
// interface here, for the reason hazard declares its own Weather and
// Geometry: no business writing hazard values, and a test of the pass should
// not need hazard's storage compiled in. Satisfied by *hazard.Engine.
type Hazard interface {
	SpanValues(
		ctx context.Context, span id.ID, quantities []hzstore.Quantity,
		issuedAt, from, to time.Time,
	) ([]hzstore.SpanValue, error)
}

// Weather is the part of the weather extension a computation needs: enough to
// turn the cycle a request names into the issuance that keys every hazard
// value this pass reads. That is the only reason this pass touches weather —
// it reads no forecast sample and no grid. Satisfied by *weather.Engine.
type Weather interface {
	Cycle(ctx context.Context, want id.ID) (forecast.Cycle, error)
}

// Geometry is the part of the geo extension a computation needs. Satisfied by
// *geo.Engine.
type Geometry interface {
	Routes(ctx context.Context, limit int) ([]geoasset.Route, error)
	Spans(ctx context.Context, route id.ID) ([]geoasset.Span, error)
	Structures(ctx context.Context, route id.ID) ([]geoasset.Structure, error)
}

// ComputeRequest asks for one pass of the risk models over one weather cycle.
type ComputeRequest struct {
	// CycleID names the cycle whose issuance keys the hazard values this
	// pass reads.
	CycleID id.ID
}

// ComputeResult is what one pass did.
type ComputeResult struct {
	// ComputationID names this pass. Minted, not stored: no computation table
	// exists yet, so this is for the caller to log, not look up.
	ComputationID id.ID

	// RunID names the provenance run. Zero when the engine has no recorder.
	RunID id.ID

	// Spans is how many spans produced a conductor probability.
	Spans int

	// Structures is how many structures produced a structure probability.
	Structures int

	// Unclassed is how many structures had a Kind that maps to no curve — a
	// gap in the asset record. Reported, not zeroed: see the package doc.
	Unclassed int

	// Unhazarded is how many spans had no hazard values to read — a gap in
	// the weather coverage, distinct from Unclassed for the reason
	// hazard.conductorFor keeps its own outcomes apart. Counted per span, not
	// per component it starves: a structure bounded solely by an unhazarded
	// span also produces nothing, but that is the same gap counted here.
	Unhazarded int

	// Values is how many probability rows were written.
	Values int

	// Scalar is what multiplied every probability in this pass.
	Scalar float64
}

// Engine holds the extension's business logic.
type Engine struct {
	store store.Store
	bus   *hooks.Bus

	// The compute half. All optional: a read-only deployment needs none.
	hazard   Hazard
	weather  Weather
	geometry Geometry
	recorder provenance.Recorder

	// scalar multiplies every probability written; defaults to
	// ScalarUncalibrated. now is injectable so a test can pin the times a
	// computation carries.
	scalar float64
	now    func() time.Time
}

// EngineOption configures the engine.
type EngineOption func(*Engine)

// WithCompute supplies what a computation pass needs. The recorder may be
// nil: a pass without one still computes and stores, and records no run.
func WithCompute(h Hazard, w Weather, g Geometry, r provenance.Recorder) EngineOption {
	return func(e *Engine) { e.hazard, e.weather, e.geometry, e.recorder = h, w, g, r }
}

// WithClock replaces the engine's clock.
func WithClock(now func() time.Time) EngineOption {
	return func(e *Engine) {
		if now != nil {
			e.now = now
		}
	}
}

// WithScalar overrides the scalar every probability is multiplied by. Values
// outside (0, 1] are ignored — rejected at the option rather than clamped at
// the write, so there is one place that decides what a valid scalar is.
// Every curve and rate this package evaluates already returns a value in
// [0, 1] (fragility bounds its own outputs), so keeping the scalar in that
// same range is what keeps a stored "probability" a probability: zero would
// zero every value regardless of the curve, and anything above 1 could push
// a curve already near its ceiling past 1, which is not a probability either.
func WithScalar(scalar float64) EngineOption {
	return func(e *Engine) {
		if scalar > 0 && scalar <= 1 {
			e.scalar = scalar
		}
	}
}

// NewEngine builds the engine.
func NewEngine(s store.Store, bus *hooks.Bus, opts ...EngineOption) (*Engine, error) {
	if s == nil {
		return nil, errors.New("risk: engine needs a store")
	}

	e := &Engine{store: s, bus: bus, now: time.Now, scalar: ScalarUncalibrated}
	for _, opt := range opts {
		opt(e)
	}

	return e, nil
}

// Compute runs the fragility models over every span and structure of every
// route, for one weather cycle, and stores the results.
//
// A structure that bounds two spans is scored at the higher of their
// loadings, not the average: it is exposed to the worst of what its adjacent
// spans see, and averaging would systematically understate one at the edge of
// a storm front.
//
// Returns an error only when nothing could succeed: no compute half, no
// cycle, no geometry, or the write failed. A single span with no hazard
// values, or structure with no curve, is counted rather than failing the pass.
func (e *Engine) Compute(ctx context.Context, in ComputeRequest) (ComputeResult, error) {
	if e.hazard == nil || e.weather == nil || e.geometry == nil {
		return ComputeResult{}, errors.New(
			"risk: computing needs hazard values, a weather cycle and geometry; " +
				"this engine was built for reads only")
	}

	startedAt := e.now().UTC()
	cycle, err := e.weather.Cycle(ctx, in.CycleID)
	if err != nil {
		return ComputeResult{}, fmt.Errorf("risk: reading cycle %s: %w", in.CycleID, err)
	}

	routes, err := e.geometry.Routes(ctx, everyRoute)
	if err != nil {
		return ComputeResult{}, fmt.Errorf("risk: listing routes: %w", err)
	}

	if len(routes) == 0 {
		return ComputeResult{}, errors.New(
			"risk: no routes to compute over; trace a route before computing a cycle")
	}

	result := ComputeResult{Scalar: e.scalar}
	values := make([]store.ComponentValue, 0)
	window := timeWindow{from: cycle.IssuedAt, to: cycle.IssuedAt.Add(widestHorizon)}

	for _, route := range routes {
		found, err := e.computeRoute(ctx, route, cycle.IssuedAt, window, &result)
		if err != nil {
			return result, err
		}

		values = append(values, found...)
	}

	if err := e.store.Values().PutBatch(ctx, values); err != nil {
		return result, err
	}

	result.Values = len(values)
	runID, err := e.recordRun(ctx, startedAt)
	if err != nil {
		return result, err
	}

	result.RunID = runID
	result.ComputationID = rid.NewComputation()
	e.emit(ctx, "risk.computation.completed", map[string]any{
		"computation_id": result.ComputationID.String(),
		"cycle_id":       in.CycleID.String(),
		"spans":          result.Spans,
		"structures":     result.Structures,
		"unclassed":      result.Unclassed,
		"unhazarded":     result.Unhazarded,
		"values":         result.Values,
	})

	return result, nil
}

// timeWindow bounds a computation pass.
type timeWindow struct{ from, to time.Time }

// computeRoute rates every span and structure of one route, folding its
// counts into result.
func (e *Engine) computeRoute(
	ctx context.Context, route geoasset.Route, issuedAt time.Time,
	window timeWindow, result *ComputeResult,
) ([]store.ComponentValue, error) {
	spans, err := e.geometry.Spans(ctx, route.ID)
	if err != nil {
		return nil, fmt.Errorf("risk: reading spans of %s: %w", route.ID, err)
	}

	structures, err := e.geometry.Structures(ctx, route.ID)
	if err != nil {
		return nil, fmt.Errorf("risk: reading structures of %s: %w", route.ID, err)
	}

	// bySequence lets a structure find the spans it bounds (structureLoad).
	// windByTime is each hazarded span's loading by moment; a span left out
	// was uncovered, reaching its structures without a second count.
	bySequence := make(map[int]geoasset.Span, len(spans))
	windByTime := make(map[id.ID]map[time.Time]float64, len(spans))
	values := make([]store.ComponentValue, 0, len(spans)+len(structures))

	for _, span := range spans {
		bySequence[span.Sequence] = span

		found, wind, err := e.computeSpan(ctx, span, issuedAt, window)
		if err != nil {
			return nil, err
		}

		if wind == nil {
			result.Unhazarded++
			continue
		}

		windByTime[span.ID] = wind
		values = append(values, found...)
		if len(found) > 0 {
			result.Spans++
		}
	}

	for _, structure := range structures {
		found, wrote := e.computeStructure(structure, issuedAt, bySequence, windByTime, result)
		if wrote {
			result.Structures++
		}
		values = append(values, found...)
	}

	return values, nil
}

// computeSpan rates one span's conductor at every hour hazard has loading
// for, returning the wind loading by moment for computeStructure to read —
// nil when uncovered, distinct from covered but nothing computed.
func (e *Engine) computeSpan(
	ctx context.Context, span geoasset.Span, issuedAt time.Time, window timeWindow,
) ([]store.ComponentValue, map[time.Time]float64, error) {
	found, err := e.hazard.SpanValues(ctx, span.ID,
		[]hzstore.Quantity{hzstore.QuantityWindLoad, hzstore.QuantityIceLoad},
		issuedAt, window.from, window.to)
	if err != nil {
		return nil, nil, fmt.Errorf(
			"risk: reading hazard values for span %s: %w", span.ID, err)
	}

	if len(found) == 0 {
		return nil, nil, nil
	}

	byTime := groupLoads(found)
	wind := make(map[time.Time]float64, len(byTime))
	values := make([]store.ComponentValue, 0, len(byTime))

	for _, at := range sortedTimes(byTime) {
		load := byTime[at]
		if !finite(load.wind, load.ice) {
			// A NaN or infinite hazard row is not a load that happened to be
			// small; storing the probability it produces would carry that
			// silently through every downstream sum instead of failing where
			// it was made. Skipped like any other moment with nothing to
			// compute — see the "moment without temperature" precedent in
			// extensions/hazard/compute.go.
			continue
		}

		wind[at] = load.wind
		probability := fragility.ConductorProbability(
			load.wind, load.ice, span.LengthM, conductorHours) * e.scalar

		values = append(values, store.ComponentValue{
			ComponentID: span.ID, Quantity: store.QuantityConductorFailure,
			IssuedAt: issuedAt, ValidAt: at,
			Probability: probability, Scalar: e.scalar,
		})
	}

	return values, wind, nil
}

// computeStructure rates one structure at every hour its bounding spans have
// wind loading for. Reports whether it wrote anything: no curve or no
// loading reaching it both write nothing.
func (e *Engine) computeStructure(
	structure geoasset.Structure, issuedAt time.Time,
	bySequence map[int]geoasset.Span, windByTime map[id.ID]map[time.Time]float64,
	result *ComputeResult,
) ([]store.ComponentValue, bool) {
	class, ok := fragility.ClassOf(structure.Kind)
	if !ok {
		result.Unclassed++

		return nil, false
	}

	curve, ok := fragility.ByClass(class)
	if !ok {
		// ClassOf only ever names a member of fragility's own closed set, so
		// this would be a bug in fragility, not a fact about the structure —
		// but it is still counted Unclassed, so the counters keep reconciling
		// to the structure count rather than quietly falling short.
		result.Unclassed++

		return nil, false
	}

	windLoads := structureLoad(structure.Ordinal, bySequence, windByTime)
	values := make([]store.ComponentValue, 0, len(windLoads))

	for _, at := range sortedTimes(windLoads) {
		values = append(values, store.ComponentValue{
			ComponentID: structure.ID, Quantity: store.QuantityStructureFailure,
			IssuedAt: issuedAt, ValidAt: at,
			Probability: curve.Probability(windLoads[at]) * e.scalar, Scalar: e.scalar,
		})
	}

	return values, len(values) > 0
}

// structureLoad returns the wind loading a structure sees at each moment its
// bounding spans have data for — the higher of the two when it bounds two,
// per Engine.Compute's doc — and nothing for a span that was uncovered.
//
// Ordinal and Sequence share an origin but not a base — Ordinal from 0,
// Sequence from 1 — so a structure's own Ordinal o bounds the spans numbered
// o and o+1 (extensions/geo/import.go documents the same offset); the first
// is absent for the route's first structure, the second for its last.
func structureLoad(
	ordinal int, bySequence map[int]geoasset.Span, windByTime map[id.ID]map[time.Time]float64,
) map[time.Time]float64 {
	out := map[time.Time]float64{}

	for _, sequence := range [2]int{ordinal, ordinal + 1} {
		span, ok := bySequence[sequence]
		if !ok {
			continue
		}

		for at, load := range windByTime[span.ID] {
			if existing, seen := out[at]; !seen || load > existing {
				out[at] = load
			}
		}
	}

	return out
}

// loads is one span's wind and ice loading at one moment.
type loads struct{ wind, ice float64 }

// groupLoads collects a span's hazard values into one wind/ice pair per
// moment, the shape ConductorProbability and a structure curve both need.
func groupLoads(values []hzstore.SpanValue) map[time.Time]loads {
	out := map[time.Time]loads{}

	for _, v := range values {
		at := v.ValidAt.UTC()
		pair := out[at]

		switch v.Quantity {
		case hzstore.QuantityWindLoad:
			pair.wind = v.Value
		case hzstore.QuantityIceLoad:
			pair.ice = v.Value
		}

		out[at] = pair
	}

	return out
}

// finite reports whether every value is a real, finite number — not NaN and
// not infinite.
func finite(values ...float64) bool {
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}

	return true
}

// sortedTimes orders a map's moments so a pass writes values in time order
// and two runs of the same cycle produce the same sequence.
func sortedTimes[T any](byTime map[time.Time]T) []time.Time {
	out := make([]time.Time, 0, len(byTime))
	for at := range byTime {
		out = append(out, at)
	}

	slices.SortFunc(out, func(left, right time.Time) int { return left.Compare(right) })

	return out
}

// recordRun records the model version and the run. No dataset-version inputs
// to cite, unlike hazard's own: this pass reads hazard's already-computed
// values, and hazard's run already carries the chain back to the dataset.
func (e *Engine) recordRun(ctx context.Context, startedAt time.Time) (id.ID, error) {
	if e.recorder == nil {
		return id.Nil, nil
	}

	build := buildinfo.Get()
	model, err := e.recorder.RecordModel(ctx, record.ModelVersion{
		Kind: record.KindModel, Name: ModelName, Version: ModelVersion,
		CodeCommit: build.Commit, RecordedAt: e.now().UTC(),
	})
	if err != nil {
		return id.Nil, fmt.Errorf("risk: recording the model version: %w", err)
	}

	run, err := e.recorder.RecordRun(ctx, record.ProvenanceRun{
		ModelVersionID: model.ID, Kind: "risk",
		StartedAt: startedAt, FinishedAt: e.now().UTC(),
	}, nil)
	if err != nil {
		return id.Nil, fmt.Errorf("risk: recording the run: %w", err)
	}

	return run.ID, nil
}

// emit publishes a domain event, best effort.
func (e *Engine) emit(ctx context.Context, name string, payload map[string]any) {
	if e.bus == nil {
		return
	}

	e.bus.Emit(ctx, hooks.Event{Name: name, Extension: ExtensionName, Payload: payload})
}
