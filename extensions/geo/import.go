package geo

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/blackstart/extensions/geo/asset"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/geo/shape"
	"github.com/xraph/blackstart/extensions/geo/source"
	"github.com/xraph/blackstart/pkg/id"
)

// ImportRequest carries a path somebody already digitized.
//
// The counterpart to TraceRequest. Trace is given two endpoints and invents
// everything between them; Import is given the path and invents only what the
// source did not record.
type ImportRequest struct {
	// CarrierID names what this path carries — almost always a circuit.
	//
	// Required, exactly as for a traced route. Imported geometry describes a
	// line somebody built and an ACTIVSg branch is synthetic, so the two must
	// not be conflated: a caller importing real geometry stands up its own
	// circuits rather than attaching a Sharyland right-of-way to a synthetic
	// branch. See the phase-3 data survey, §1.5, and the real-circuit-identity
	// design, §2.
	CarrierID id.ID

	// Path is the digitized right-of-way, in order.
	Path shape.Line

	// Structures are the recorded structures along Path, in order, with
	// whatever the source said about each.
	//
	// Empty when the source publishes lines but not structures, which is the
	// usual case: the transmission layers republished from HIFLD are polylines
	// whose vertices are digitizing points, not towers. Supplying them is what
	// makes the result Surveyed rather than Routed.
	Structures []source.Structure

	// SpanM overrides the nominal distance between interpolated structures.
	// Ignored when Structures is supplied, because then nothing is being
	// interpolated. Zero uses the engine's configured default.
	SpanM float64
}

// Import stores a route whose path somebody else recorded.
//
// The provenance is decided here rather than passed in, because it is a
// statement about the data and a caller should not be able to assert it. A
// request carrying structures is Surveyed; one carrying only a path is Routed.
// Nothing a caller says makes an interpolated structure a real one.
//
// # Errors
//
// Returns an error when the carrier is missing, the path is not a line, the
// supplied structures do not lie along it, or the route cannot be stored.
func (e *Engine) Import(ctx context.Context, in ImportRequest) (asset.Route, error) {
	if in.CarrierID.IsZero() {
		return asset.Route{}, errors.New(
			"geo: an imported route needs the circuit it carries; a path over " +
				"the ground that carries nothing is not part of the network")
	}

	path, provenance, err := e.resolveGeometry(in)
	if err != nil {
		return asset.Route{}, fmt.Errorf("geo: importing %s: %w", in.CarrierID, err)
	}

	route := asset.Route{
		ID:         gid.NewRoute(),
		CarrierID:  in.CarrierID,
		Provenance: provenance,
		LengthM:    path.LengthM(),
	}

	segments := path.Spans()
	spans := make([]asset.Span, 0, len(segments))

	for index, segment := range segments {
		spans = append(spans, asset.Span{
			ID:         gid.NewSpan(),
			RouteID:    route.ID,
			Sequence:   index + 1,
			Geometry:   segment,
			LengthM:    segment.LengthM(),
			AzimuthDeg: segment[0].AzimuthTo(segment[len(segment)-1]),
		})
	}

	if err := e.store.Routes().Put(ctx, route, spans); err != nil {
		return asset.Route{}, fmt.Errorf("geo: storing the imported route for %s: %w", in.CarrierID, err)
	}

	// Ordinal comes from the loop index, not from any sort: it is the source's
	// own order, which is the same order resolveGeometry used to build path
	// and the same order Spans() just numbered above it. Structure n is where
	// span n begins, and sorting here would break that correspondence
	// silently — nothing downstream could tell the two orders had diverged.
	structures := make([]asset.Structure, 0, len(in.Structures))

	for ordinal, recorded := range in.Structures {
		structures = append(structures, asset.Structure{
			ID:       gid.NewStructure(),
			RouteID:  route.ID,
			Ordinal:  ordinal,
			Location: recorded.Position,
			Kind:     recorded.Kind,
			Material: recorded.Material,
		})
	}

	if err := e.store.Structures().Put(ctx, route.ID, structures); err != nil {
		return asset.Route{}, fmt.Errorf("geo: storing structures: %w", err)
	}

	e.emit(ctx, route, len(spans))

	return route, nil
}

// resolveGeometry decides what the stored geometry is and what it may claim.
func (e *Engine) resolveGeometry(in ImportRequest) (shape.Line, asset.Provenance, error) {
	if err := in.Path.Validate(); err != nil {
		return nil, "", err
	}

	if len(in.Structures) == 0 {
		spanM := in.SpanM
		if spanM <= 0 {
			spanM = e.spanM
		}

		// The path is real and the structures are not, which is what Routed
		// says. Resample keeps the digitized corners rather than straightening
		// them onto a great circle.
		path, err := shape.Resample(in.Path, spanM)
		if err != nil {
			return nil, "", err
		}

		return path, asset.Routed, nil
	}

	positions := make(shape.Line, len(in.Structures))
	for index, recorded := range in.Structures {
		positions[index] = recorded.Position
	}

	if err := positions.Validate(); err != nil {
		return nil, "", fmt.Errorf("the supplied structures are not a line: %w", err)
	}

	// Recorded structures become the geometry directly: a span runs between
	// two towers, so the towers are the vertices. Resampling here would insert
	// imaginary structures between real ones and quietly downgrade the claim
	// while still calling it Surveyed.
	return positions, asset.Surveyed, nil
}

// ImportLine stores a circuit and its geometry from a published source.
//
// The call the two fetchers in extensions/geo/source exist to feed, and the
// one that makes an import re-runnable. The circuit is reconciled on the
// source's own identity, so re-importing the same bounding box updates what is
// there rather than doubling it — and the route follows the circuit's existing
// identifier, so geometry is never orphaned by a fresh one.
//
// Both halves are decided from the data, not from the caller. Which circuit
// this is comes from the source's identifier; whether the structures are real
// comes from whether the source published them.
//
// # Errors
//
// Returns an error when the line has no external identifier, its path is not a
// line, or either the circuit or the route cannot be stored.
func (e *Engine) ImportLine(ctx context.Context, line source.Line) (asset.Circuit, asset.Route, error) {
	if err := line.Validate(); err != nil {
		return asset.Circuit{}, asset.Route{}, err
	}

	circuit, err := e.store.Circuits().Reconcile(ctx, asset.Circuit{
		// Used only if the source's key is new. Reconcile keeps the existing
		// identifier when it is not, which is the whole point of the key.
		ID:         gid.NewCircuit(),
		ExternalID: line.ExternalID,
		Source:     line.Source,
		VoltageKV:  line.VoltageKV,
		Operator:   line.Owner,
	})
	if err != nil {
		return asset.Circuit{}, asset.Route{}, fmt.Errorf(
			"geo: reconciling %s from %s: %w", line.ExternalID, line.Source, err)
	}

	route, err := e.Import(ctx, ImportRequest{
		CarrierID:  circuit.ID,
		Path:       line.Path,
		Structures: line.Structures,
	})
	if err != nil {
		return asset.Circuit{}, asset.Route{}, err
	}

	return circuit, route, nil
}

// Circuit reads one circuit.
func (e *Engine) Circuit(ctx context.Context, want id.ID) (asset.Circuit, error) {
	return e.store.Circuits().Get(ctx, want)
}

// Circuits lists imported circuits.
func (e *Engine) Circuits(ctx context.Context, limit int) ([]asset.Circuit, error) {
	return e.store.Circuits().List(ctx, limit)
}
