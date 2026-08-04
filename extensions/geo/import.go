package geo

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/blackstart/extensions/geo/asset"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/geo/shape"
	"github.com/xraph/blackstart/pkg/id"
)

// ImportRequest carries a path somebody already digitised.
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

	// Path is the digitised right-of-way, in order.
	Path shape.Line

	// Structures are the recorded structure positions along Path, in order.
	//
	// Empty when the source publishes lines but not structures, which is the
	// usual case: the transmission layers republished from HIFLD are polylines
	// whose vertices are digitising points, not towers. Supplying them is what
	// makes the result Surveyed rather than Routed.
	Structures shape.Line

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
		// says. Resample keeps the digitised corners rather than straightening
		// them onto a great circle.
		path, err := shape.Resample(in.Path, spanM)
		if err != nil {
			return nil, "", err
		}

		return path, asset.Routed, nil
	}

	if err := in.Structures.Validate(); err != nil {
		return nil, "", fmt.Errorf("the supplied structures are not a line: %w", err)
	}

	// Recorded structures become the geometry directly: a span runs between
	// two towers, so the towers are the vertices. Resampling here would insert
	// imaginary structures between real ones and quietly downgrade the claim
	// while still calling it Surveyed.
	return in.Structures, asset.Surveyed, nil
}
