package fabriq

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xraph/grove"

	"github.com/xraph/blackstart/extensions/weather/field"
	"github.com/xraph/blackstart/extensions/weather/forecast"
	wid "github.com/xraph/blackstart/extensions/weather/id"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/store/fabriqstore"
)

// gridModel is the persistence shape of one retrieved GRIB2 message set.
type gridModel struct {
	grove.BaseModel `grove:"table:blackstart_weather_grid"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	CycleID      string `grove:"cycle_id,notnull"`
	ForecastHour int    `grove:"forecast_hour,notnull"`
	Field        string `grove:"field,notnull"`

	BlobRef          string `grove:"blob_ref,notnull"`
	DatasetVersionID string `grove:"dataset_version_id,notnull"`

	// The grid's shape is not known when this row is written. A grid is
	// created as soon as its bytes are stored and its dataset version
	// recorded, which is before anything has decoded them — so nx, ny and
	// projection are filled in by the sampling pass, and none of them may be
	// declared notnull here.
	//
	// The distinction matters because fabriq's notnull is stricter than the
	// column's: the DDL says NOT NULL DEFAULT '', which an empty string
	// satisfies, but fabriq refuses an empty value for a notnull field
	// outright. Tagging projection notnull made every pre-decode create fail.
	NX         int    `grove:"nx"`
	NY         int    `grove:"ny"`
	Projection string `grove:"projection"`

	// SampledAt is a pointer because the column is nullable. A grid that has
	// never been sampled has no such time, and a zero timestamp would read as
	// the year 1 — a value, and a wrong one — rather than as absence.
	SampledAt *time.Time `grove:"sampled_at"`

	Covered int `grove:"covered,notnull"`
	Total   int `grove:"total,notnull"`
}

func gridToModel(g forecast.Grid) *gridModel {
	m := &gridModel{
		ID:               g.ID.String(),
		CycleID:          g.CycleID.String(),
		ForecastHour:     g.ForecastHour,
		Field:            string(g.Field),
		BlobRef:          g.BlobRef,
		DatasetVersionID: g.DatasetVersionID.String(),
		NX:               g.NX,
		NY:               g.NY,
		Projection:       g.Projection,
		Covered:          g.Covered,
		Total:            g.Total,
	}

	if !g.SampledAt.IsZero() {
		at := g.SampledAt.UTC()
		m.SampledAt = &at
	}

	return m
}

func gridFromModel(m gridModel) (forecast.Grid, error) {
	gridID, err := wid.ParseGrid(m.ID)
	if err != nil {
		return forecast.Grid{}, fmt.Errorf("weather: stored grid id %q: %w", m.ID, err)
	}

	cycleID, err := wid.ParseCycle(m.CycleID)
	if err != nil {
		return forecast.Grid{}, fmt.Errorf(
			"weather: stored cycle id %q: %w", m.CycleID, err)
	}

	datasetID, err := id.Parse(m.DatasetVersionID)
	if err != nil {
		return forecast.Grid{}, fmt.Errorf(
			"weather: stored dataset version id %q: %w", m.DatasetVersionID, err)
	}

	out := forecast.Grid{
		ID:               gridID,
		CycleID:          cycleID,
		ForecastHour:     m.ForecastHour,
		Field:            field.Field(m.Field),
		BlobRef:          m.BlobRef,
		DatasetVersionID: datasetID,
		NX:               m.NX,
		NY:               m.NY,
		Projection:       m.Projection,
		Covered:          m.Covered,
		Total:            m.Total,
	}

	if m.SampledAt != nil {
		out.SampledAt = m.SampledAt.UTC()
	}

	return out, nil
}

type gridStore struct {
	base *fabriqstore.Base
}

func (s *gridStore) Create(ctx context.Context, in forecast.Grid) error {
	if err := in.Validate(); err != nil {
		return err
	}

	err := s.base.Create(ctx, GridEntity, in.ID, gridToModel(in))
	if err != nil {
		// One retrieval per field per hour per cycle. A second row would leave
		// two dataset versions for one field with nothing to say which the
		// samples came from.
		if errors.Is(err, bserr.ErrConflict) {
			return fmt.Errorf(
				"%w: cycle %s already has %s at hour %d",
				bserr.ErrConflict, in.CycleID, in.Field, in.ForecastHour)
		}

		return err
	}

	return nil
}

func (s *gridStore) Get(ctx context.Context, want id.ID) (forecast.Grid, error) {
	var m gridModel

	if err := s.base.Get(ctx, GridEntity, want, &m); err != nil {
		return forecast.Grid{}, err
	}

	return gridFromModel(m)
}

func (s *gridStore) ForCycle(ctx context.Context, cycle id.ID) ([]forecast.Grid, error) {
	var rows []gridModel

	// Ordered in SQL rather than in Go, because the index is already on
	// (tenant_id, cycle_id, forecast_hour, field) and re-sorting a cycle's
	// worth of rows in memory would discard that.
	if err := s.base.Query(ctx, &rows,
		`SELECT * FROM `+GridTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND cycle_id = $1
		 ORDER BY forecast_hour ASC, field ASC`, cycle.String()); err != nil {
		return nil, err
	}

	out := make([]forecast.Grid, 0, len(rows))

	for _, row := range rows {
		got, err := gridFromModel(row)
		if err != nil {
			return nil, err
		}

		out = append(out, got)
	}

	return out, nil
}

func (s *gridStore) MarkSampled(
	ctx context.Context, want id.ID, at time.Time, covered, total int,
) error {
	if covered > total {
		return fmt.Errorf(
			"%w: grid %s covered %d of %d spans; more covered than offered means "+
				"the sampler counted something twice",
			bserr.ErrInvalid, want, covered, total)
	}

	// Read-modify-write, because the command path replaces the aggregate: a
	// partial payload would blank the blob reference and the dataset version,
	// which are the two fields that make the samples traceable.
	existing, err := s.Get(ctx, want)
	if err != nil {
		return err
	}

	existing.SampledAt = at.UTC()
	existing.Covered = covered
	existing.Total = total

	return s.base.Update(ctx, GridEntity, want, gridToModel(existing), nil)
}
