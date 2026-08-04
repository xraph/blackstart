package fabriq

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/xraph/fabriq/core/query"

	"github.com/xraph/blackstart/extensions/weather/forecast"
	"github.com/xraph/blackstart/extensions/weather/observation"
	"github.com/xraph/blackstart/pkg/store/fabriqstore"
)

// sampleStore is the only thing in this platform that writes a hypertable.
//
// Two properties of that make it unlike every other store here, and both are
// deliberate rather than worked around.
//
// Writes bypass the outbox. fabriq's Timeseries port documents BulkWrite as
// the event-bypass ingest path — per-row outbox events would melt the outbox,
// so bulk telemetry skips it and the relay publishes conflated deltas instead.
// "All entity writes go through fabriq" still holds; "all data writes" does
// not, by design, and per-span weather is exactly the case the exception was
// built for.
//
// Reads carry their own tenant predicate. The series tables have no row-level
// security, because Timescale's columnstore forbids it, and fabriq's raw-SQL
// guard will not cover them either: its table list comes from an unexported
// constant naming only fabriq's own demo table. So the
// `tenant_id = current_setting(...)` in the two queries below is the whole of
// tenant isolation on this data. It is written once, here, for that reason.
type sampleStore struct {
	base *fabriqstore.Base
}

func (s *sampleStore) WriteForecast(
	ctx context.Context, samples []forecast.Sample,
) error {
	// Validated before anything is written. BulkWrite is one multi-row INSERT
	// and therefore atomic per call, so refusing the batch up front is what
	// keeps a bad sample from being the difference between all of it and none.
	points := make([]query.Point, 0, len(samples))

	for index, sample := range samples {
		if err := sample.Validate(); err != nil {
			return fmt.Errorf("weather: sample %d: %w", index, err)
		}

		points = append(points, query.Point{
			Key: forecast.SampleKey(sample.SpanID, sample.Field, sample.IssuedAt),
			At:  sample.ValidAt.UTC(),
			// Value as the source reported it, in the unit the field's spec
			// names. Quality stays zero: GRIB2 carries no per-point quality
			// flag, and inventing one would be a number nothing produced.
			Value: sample.Value,
		})
	}

	return s.bulkWrite(ctx, ForecastSeries, points)
}

func (s *sampleStore) ReadForecast(
	ctx context.Context, keys []string, from, to time.Time,
) ([]forecast.Sample, error) {
	rows, err := s.readSeries(ctx, ForecastSeries, keys, from, to)
	if err != nil {
		return nil, err
	}

	out := make([]forecast.Sample, 0, len(rows))

	for _, row := range rows {
		// The span, the field and the issuance live only in the key — the
		// series table stores one key and one float per row — so parsing it is
		// how a stored point becomes a Sample again.
		spanID, got, issuedAt, err := forecast.ParseSampleKey(row.Key)
		if err != nil {
			return nil, fmt.Errorf(
				"weather: reading %s: %w", ForecastSeries, err)
		}

		out = append(out, forecast.Sample{
			SpanID:   spanID,
			Field:    got,
			IssuedAt: issuedAt,
			ValidAt:  row.At.UTC(),
			Value:    row.Value,
		})
	}

	return out, nil
}

func (s *sampleStore) WriteObservations(
	ctx context.Context, obs []observation.Observation,
) error {
	points := make([]query.Point, 0, len(obs))

	for index, reading := range obs {
		if err := reading.Validate(); err != nil {
			return fmt.Errorf("weather: observation %d: %w", index, err)
		}

		points = append(points, query.Point{
			Key:   observation.SampleKey(reading.StationID, reading.Field),
			At:    reading.ObservedAt.UTC(),
			Value: reading.Value,
		})
	}

	return s.bulkWrite(ctx, ObservationSeries, points)
}

func (s *sampleStore) ReadObservations(
	ctx context.Context, keys []string, from, to time.Time,
) ([]observation.Observation, error) {
	rows, err := s.readSeries(ctx, ObservationSeries, keys, from, to)
	if err != nil {
		return nil, err
	}

	out := make([]observation.Observation, 0, len(rows))

	for _, row := range rows {
		stationID, got, err := observation.ParseSampleKey(row.Key)
		if err != nil {
			return nil, fmt.Errorf(
				"weather: reading %s: %w", ObservationSeries, err)
		}

		out = append(out, observation.Observation{
			StationID:  stationID,
			Field:      got,
			ObservedAt: row.At.UTC(),
			Value:      row.Value,
		})
	}

	return out, nil
}

// bulkWrite sends points through fabriq's Timeseries port.
//
// Not through fabriqstore.Base, which wraps the command path: this is the one
// write in the extension that deliberately does not go through it.
func (s *sampleStore) bulkWrite(
	ctx context.Context, series string, points []query.Point,
) error {
	if len(points) == 0 {
		return nil
	}

	ts := s.base.Fabric().Timeseries()
	if ts == nil {
		return fmt.Errorf(
			"weather: no timeseries port; the database has no hypertable "+
				"support, so %s samples cannot be stored", series)
	}

	if err := ts.BulkWrite(s.base.Scoped(ctx), series, points); err != nil {
		return fmt.Errorf("weather: writing %d points to %s: %w",
			len(points), series, fabriqstore.Translate(err))
	}

	return nil
}

// seriesRow is one stored point, as the read below scans it.
type seriesRow struct {
	Key   string    `grove:"tag_id"`
	At    time.Time `grove:"time"`
	Value float64   `grove:"value"`
}

// readSeries reads points for a set of keys over the half-open [from, to).
//
// One query for every key, rather than the port's Range, which takes a single
// key and would make "every field of every span on this route" N round trips.
// The cost of doing it here is that the tenant predicate is ours to write —
// see the type comment.
//
// Half-open so that a caller walking a series hour by hour does not read every
// boundary twice.
func (s *sampleStore) readSeries(
	ctx context.Context, series string, keys []string, from, to time.Time,
) ([]seriesRow, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	if !to.After(from) {
		return nil, fmt.Errorf(
			"weather: reading %s over [%s, %s): the window ends at or before it "+
				"begins, so it selects nothing", series,
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

	// The tenant predicate is not decoration. These tables are outside RLS and
	// outside fabriq's raw-SQL guard, so removing this line would silently
	// return every tenant's weather rather than failing.
	if err := s.base.Query(ctx, &rows,
		`SELECT tag_id, time, value FROM `+series+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND time >= $1 AND time < $2
		   AND tag_id IN (`+string(placeholder)+`)
		 ORDER BY time ASC, tag_id ASC`, args...); err != nil {
		return nil, err
	}

	return rows, nil
}
