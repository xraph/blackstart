package fabriq

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/xraph/fabriq/core/query"

	"github.com/xraph/blackstart/extensions/hazard/store"
	"github.com/xraph/blackstart/pkg/store/fabriqstore"
)

// valueStore writes the hazard hypertable.
//
// Two properties of that make it unlike every other store here, and both are
// deliberate rather than worked around. They are the same two the weather
// extension's sample store carries, for the same reasons.
//
// Writes bypass the outbox. fabriq's Timeseries port documents BulkWrite as the
// event-bypass ingest path — per-row outbox events would melt the outbox, so
// bulk telemetry skips it and the relay publishes conflated deltas instead.
// "All entity writes go through fabriq" still holds; "all data writes" does
// not, by design.
//
// Reads carry their own tenant predicate. The series table has no row-level
// security, because Timescale's columnstore forbids it, and fabriq's raw-SQL
// guard will not cover it either: its table list comes from an unexported
// constant naming only fabriq's own demo table. So the
// `tenant_id = current_setting(...)` in the read below is the whole of tenant
// isolation on this data. It is written once, here, for that reason.
type valueStore struct {
	base *fabriqstore.Base
}

func (s *valueStore) Write(ctx context.Context, values []store.SpanValue) error {
	// Validated before anything is written. BulkWrite is one multi-row INSERT
	// and therefore atomic per call, so refusing the batch up front is what
	// keeps a bad value from being the difference between all of it and none.
	points := make([]query.Point, 0, len(values))

	for index, value := range values {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("hazard: value %d: %w", index, err)
		}

		points = append(points, query.Point{
			Key: value.Key(),
			At:  value.ValidAt.UTC(),
			// Quality stays zero. A derived value is exactly as good as the
			// forecast it came from, and that provenance lives on the
			// computation row rather than as a number invented here.
			Value: value.Value,
		})
	}

	if len(points) == 0 {
		return nil
	}

	ts := s.base.Fabric().Timeseries()
	if ts == nil {
		return fmt.Errorf(
			"hazard: no timeseries port; the database has no hypertable "+
				"support, so %s values cannot be stored", ValueSeries)
	}

	if err := ts.BulkWrite(s.base.Scoped(ctx), ValueSeries, points); err != nil {
		return fmt.Errorf("hazard: writing %d points to %s: %w",
			len(points), ValueSeries, fabriqstore.Translate(err))
	}

	return nil
}

// seriesRow is one stored point, as the read below scans it.
type seriesRow struct {
	Key   string    `grove:"tag_id"`
	At    time.Time `grove:"time"`
	Value float64   `grove:"value"`
}

// Read reads values for a set of keys over the half-open [from, to).
//
// One query for every key, rather than the port's Range, which takes a single
// key and would make "every quantity of every span on this route" N round
// trips. The cost of doing it here is that the tenant predicate is ours to
// write — see the type comment.
//
// Half-open so that a caller walking a series hour by hour does not read every
// boundary twice.
func (s *valueStore) Read(
	ctx context.Context, keys []string, from, to time.Time,
) ([]store.SpanValue, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	if !to.After(from) {
		return nil, fmt.Errorf(
			"hazard: reading %s over [%s, %s): the window ends at or before it "+
				"begins, so it selects nothing", ValueSeries,
			from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339))
	}

	args := make([]any, 0, len(keys)+2)
	args = append(args, from.UTC(), to.UTC())

	placeholder := make([]byte, 0, len(keys)*4)

	for index, key := range keys {
		if index > 0 {
			placeholder = append(placeholder, ',')
		}

		args = append(args, key)
		placeholder = append(placeholder, '$')
		placeholder = append(placeholder, strconv.Itoa(len(args))...)
	}

	var rows []seriesRow

	// The tenant predicate is not decoration. This table is outside RLS and
	// outside fabriq's raw-SQL guard, so removing this line would silently
	// return every tenant's ratings rather than failing.
	if err := s.base.Query(ctx, &rows,
		`SELECT tag_id, time, value FROM `+ValueSeries+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND time >= $1 AND time < $2
		   AND tag_id IN (`+string(placeholder)+`)
		 ORDER BY time ASC, tag_id ASC`, args...); err != nil {
		return nil, err
	}

	out := make([]store.SpanValue, 0, len(rows))

	for _, row := range rows {
		// The span, the quantity and the issuance live only in the key — the
		// series table stores one key and one float per row — so parsing it is
		// how a stored point becomes a SpanValue again.
		spanID, quantity, issuedAt, err := store.ParseValueKey(row.Key)
		if err != nil {
			return nil, fmt.Errorf("hazard: reading %s: %w", ValueSeries, err)
		}

		out = append(out, store.SpanValue{
			SpanID:   spanID,
			Quantity: quantity,
			IssuedAt: issuedAt,
			ValidAt:  row.At.UTC(),
			Value:    row.Value,
		})
	}

	return out, nil
}
