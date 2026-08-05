package scenario

import (
	"context"
	"errors"
	"fmt"
	"time"

	hazardstore "github.com/xraph/blackstart/extensions/hazard/store"
	"github.com/xraph/blackstart/pkg/id"
)

// SeriesReader is the one method of hazard's value store this reads through.
//
// Narrow for the reason GeoReader is: the rating step has no business writing
// a hazard value, and an interface that could would put the extension that
// consumes ratings one typo away from producing them.
type SeriesReader interface {
	// Read reads span values for the given series keys, over the half-open
	// window [from, to).
	Read(
		ctx context.Context, keys []string, from, to time.Time,
	) ([]hazardstore.SpanValue, error)
}

// hazard's own store satisfies it, checked here rather than asserted in
// prose — a comment claiming the shapes match is enforced by nothing.
var _ SeriesReader = (hazardstore.ValueStore)(nil)

// SpanAmpacities reads computed ampacities out of hazard's series store.
//
// The adapter between the two halves of the rating chain. hazard stores a
// number per span per forecast issuance per valid time; the rating step wants
// "the ampacity of these spans, from this cycle, at this moment", which is
// one keyed read rather than a query.
type SpanAmpacities struct {
	series SeriesReader
}

// This is the HazardReader the engine runs against in production; the tests
// fake the interface, so without this assertion nothing would notice the
// shipped adapter drifting out of shape.
var _ HazardReader = (*SpanAmpacities)(nil)

// NewSpanAmpacities adapts a hazard value store to HazardReader.
func NewSpanAmpacities(series SeriesReader) (*SpanAmpacities, error) {
	if series == nil {
		return nil, errors.New(
			"scenario: reading ampacities needs hazard's value store")
	}

	return &SpanAmpacities{series: series}, nil
}

// instant is the width of the read window, and the resolution it aligns to.
//
// A microsecond because that is what a TIMESTAMPTZ column stores: hazard's
// series table declares `time TIMESTAMPTZ` and its read is
// `time >= $1 AND time < $2`. A nanosecond-wide window puts both bounds on
// the same microsecond on the wire, so the predicate becomes
// `time >= X AND time < X` and matches nothing — every span comes back
// unrated and the run fails blaming the forecast for an arithmetic mistake in
// this file. That was the first version of this constant, and only a test
// against the real store could have caught it.
//
// One microsecond and no wider. A hazard series is keyed by (span, quantity,
// issuance) with the valid time as the position inside it, so a window
// covering more than one instant returns several members of one series —
// several forecast hours — and something downstream would then have to pick
// among them. Picking silently is the wrong-forecast-hour failure the
// manifest pins two time axes to prevent, and it would be invisible: every
// number returned is a real ampacity for a real moment, just not the one that
// was asked for.
const instant = time.Microsecond

// Ampacities reads the thermal rating computed for each span, in AMPERES.
//
// A span with no value at exactly this instant is absent from the result
// rather than present with a zero, which is what leaves its branch on the
// static rating instead of removing that branch from every violation check.
//
// # Errors
//
// Returns an error when the store cannot answer, and when one span has two
// disagreeing values at the same instant. The second is not defensive
// paranoia: hazard's memory store appends without upsert and neither store
// orders values that share a timestamp, so two computation passes over one
// cycle can leave a series that reads back differently each time. Choosing
// between them here would make the same pinned manifest produce different
// ratings on different runs, which is the one property this extension exists
// to guarantee.
func (a *SpanAmpacities) Ampacities(
	ctx context.Context, spans []id.ID, issuedAt, validAt time.Time,
) (map[id.ID]float64, error) {
	if len(spans) == 0 {
		return map[id.ID]float64{}, nil
	}

	keys := make([]string, 0, len(spans))
	for _, span := range spans {
		keys = append(keys, hazardstore.ValueKey(span, hazardstore.QuantityAmpacity, issuedAt))
	}

	// Aligned to the column's resolution rather than used as given. A valid
	// time carrying sub-microsecond precision is truncated by the driver on
	// the way in, so a window starting at the untruncated value would begin
	// after the row it is looking for.
	at := validAt.Truncate(instant)

	values, err := a.series.Read(ctx, keys, at, at.Add(instant))
	if err != nil {
		return nil, fmt.Errorf(
			"scenario: reading the ampacity of %d span(s) from the forecast issued "+
				"at %s, valid at %s: %w", len(spans),
			issuedAt.UTC().Format(time.RFC3339), validAt.UTC().Format(time.RFC3339), err)
	}

	out := make(map[id.ID]float64, len(values))

	for _, value := range values {
		// Checked rather than trusted to the window. A store whose read is
		// inclusive at the far end would return a neighboring forecast hour
		// that the caller cannot tell apart from the pinned one. Compared at
		// the column's resolution, on both sides, for the reason the window
		// is aligned to it: an equality against an untruncated valid time
		// would reject the very row that was asked for.
		if !value.ValidAt.Truncate(instant).Equal(at) {
			continue
		}

		if existing, seen := out[value.SpanID]; seen && existing != value.Value {
			return nil, fmt.Errorf(
				"scenario: span %s has two ampacities at %s from the forecast issued "+
					"at %s (%v A and %v A); a run cannot choose between them and "+
					"stay reproducible", value.SpanID,
				validAt.UTC().Format(time.RFC3339), issuedAt.UTC().Format(time.RFC3339),
				existing, value.Value)
		}

		out[value.SpanID] = value.Value
	}

	return out, nil
}
