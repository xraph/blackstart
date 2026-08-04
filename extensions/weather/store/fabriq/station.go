package fabriq

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/xraph/grove"

	"github.com/xraph/blackstart/extensions/geo/shape"
	wid "github.com/xraph/blackstart/extensions/weather/id"
	"github.com/xraph/blackstart/extensions/weather/observation"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	pkgstore "github.com/xraph/blackstart/pkg/store"
	"github.com/xraph/blackstart/pkg/store/fabriqstore"
)

// stationModel is the persistence shape of a surface observing site.
//
// The ordinates are columns rather than well-known text, which is where this
// departs from geo's spans. Those store WKT because a LineString has many
// points and re-encoding is the only way to keep one whole; a station is a
// single point, so two floats are the durable copy and are directly queryable
// besides.
//
// The PostGIS row Fabriq's spatial port writes is a second copy, and a
// rebuildable index over these columns rather than the only record — the
// spatial write is direct rather than part of the aggregate's transaction, so
// a crash between them must not lose the location.
type stationModel struct {
	grove.BaseModel `grove:"table:blackstart_weather_station"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	Code string `grove:"code,notnull"`
	Name string `grove:"name"`

	Lon       float64 `grove:"lon,notnull"`
	Lat       float64 `grove:"lat,notnull"`
	Elevation float64 `grove:"elevation,notnull"`
}

func stationToModel(s observation.Station) *stationModel {
	return &stationModel{
		ID:        s.ID.String(),
		Code:      s.Code,
		Name:      s.Name,
		Lon:       s.Location.Lon,
		Lat:       s.Location.Lat,
		Elevation: s.Location.Elevation,
	}
}

func stationFromModel(m stationModel) (observation.Station, error) {
	stationID, err := wid.ParseStation(m.ID)
	if err != nil {
		return observation.Station{}, fmt.Errorf(
			"weather: stored station id %q: %w", m.ID, err)
	}

	return observation.Station{
		ID:   stationID,
		Code: m.Code,
		Name: m.Name,
		Location: shape.Point{
			Lon:       m.Lon,
			Lat:       m.Lat,
			Elevation: m.Elevation,
		},
	}, nil
}

type stationStore struct {
	base *fabriqstore.Base
}

// Put writes a station, replacing any existing one.
//
// The row goes through the command path and the geometry through the spatial
// port, in that order. The ordering matters for the same reason it does in
// geo: a crash between them leaves a station whose location is missing from
// the index but present on its own row, which a reindex can replay. The
// reverse would leave indexed geometry for a station that does not exist.
func (s *stationStore) Put(ctx context.Context, in observation.Station) error {
	if err := in.Validate(); err != nil {
		return err
	}

	model := stationToModel(in)

	// Create, then fall back to update. The station list is re-seeded from the
	// publisher, so most writes are repeats of unchanged rows and a collision
	// is the expected case rather than an error.
	err := s.base.Create(ctx, StationEntity, in.ID, model)
	if errors.Is(err, bserr.ErrConflict) {
		err = s.base.Update(ctx, StationEntity, in.ID, model, nil)
	}

	if err != nil {
		return err
	}

	return s.base.Spatial(ctx, StationEntity, in.ID, in.Location.EWKT(), shape.SRID,
		map[string]any{"code": in.Code})
}

func (s *stationStore) Get(ctx context.Context, want id.ID) (observation.Station, error) {
	var m stationModel

	if err := s.base.Get(ctx, StationEntity, want, &m); err != nil {
		return observation.Station{}, err
	}

	return stationFromModel(m)
}

func (s *stationStore) ByCode(
	ctx context.Context, code string,
) (observation.Station, error) {
	var rows []stationModel

	if err := s.base.Query(ctx, &rows,
		`SELECT * FROM `+StationTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND code = $1
		 LIMIT 1`, code); err != nil {
		return observation.Station{}, err
	}

	if len(rows) == 0 {
		return observation.Station{}, fmt.Errorf(
			"%w: no station with code %q", bserr.ErrNotFound, code)
	}

	return stationFromModel(rows[0])
}

func (s *stationStore) List(
	ctx context.Context, p pkgstore.Page,
) ([]observation.Station, error) {
	p = p.Normalize()

	var rows []stationModel

	// Keyset pagination over the identifier, which sorts chronologically. An
	// offset would re-scan everything it skipped and would drop or repeat rows
	// when the set changes mid-walk — and the set does change, on every reseed.
	sql := `SELECT * FROM ` + StationTable + `
		 WHERE tenant_id = current_setting('app.tenant_id', true)`

	args := make([]any, 0, 2)

	// Bound rather than interpolated, both of them. The limit is an int that
	// Normalize has already clamped, so interpolating it would be safe today —
	// but this file is the one place tenant isolation on these tables is
	// enforced by hand, and a string-built predicate here is the habit worth
	// not forming.
	if !p.IsFirst() {
		args = append(args, p.After.String())
		sql += ` AND id > $` + strconv.Itoa(len(args))
	}

	args = append(args, p.Limit)
	sql += ` ORDER BY id ASC LIMIT $` + strconv.Itoa(len(args))

	if err := s.base.Query(ctx, &rows, sql, args...); err != nil {
		return nil, err
	}

	out := make([]observation.Station, 0, len(rows))

	for _, row := range rows {
		got, err := stationFromModel(row)
		if err != nil {
			return nil, err
		}

		out = append(out, got)
	}

	return out, nil
}
