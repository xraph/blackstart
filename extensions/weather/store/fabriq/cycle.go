package fabriq

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xraph/grove"

	"github.com/xraph/fabriq/core/query"

	"github.com/xraph/blackstart/extensions/weather/forecast"
	wid "github.com/xraph/blackstart/extensions/weather/id"
	"github.com/xraph/blackstart/extensions/weather/store"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	pkgstore "github.com/xraph/blackstart/pkg/store"
	"github.com/xraph/blackstart/pkg/store/fabriqstore"
)

// cycleModel is the persistence shape of a model run.
//
// The first three fields are Fabriq's structural contract: every entity
// declares id, tenant_id and version, and the registry refuses a model that
// does not. They are stamped by the executor, so caller values are ignored.
type cycleModel struct {
	grove.BaseModel `grove:"table:blackstart_weather_cycle"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	// FeedID is the column the graph projection follows, tying a retrieval to
	// its registry entry and therefore to a license.
	FeedID string `grove:"feed_id,notnull"`

	Model    string    `grove:"model,notnull"`
	IssuedAt time.Time `grove:"issued_at,notnull"`
	Status   string    `grove:"status,notnull"`
}

func cycleToModel(c forecast.Cycle) *cycleModel {
	return &cycleModel{
		ID:     c.ID.String(),
		FeedID: c.FeedID.String(),
		Model:  string(c.Model),
		// Stored in UTC. A timestamptz round trip preserves the instant but not
		// the zone, and an issuance compared against one read back in another
		// zone is a comparison nobody can reason about — including the unique
		// index, which is what makes a cycle idempotent.
		IssuedAt: c.IssuedAt.UTC(),
		Status:   string(c.Status),
	}
}

func cycleFromModel(m cycleModel) (forecast.Cycle, error) {
	cycleID, err := wid.ParseCycle(m.ID)
	if err != nil {
		return forecast.Cycle{}, fmt.Errorf("weather: stored cycle id %q: %w", m.ID, err)
	}

	feedID, err := id.Parse(m.FeedID)
	if err != nil {
		return forecast.Cycle{}, fmt.Errorf("weather: stored feed id %q: %w", m.FeedID, err)
	}

	return forecast.Cycle{
		ID:       cycleID,
		FeedID:   feedID,
		Model:    forecast.Model(m.Model),
		IssuedAt: m.IssuedAt.UTC(),
		Status:   forecast.CycleStatus(m.Status),
	}, nil
}

type cycleStore struct {
	base *fabriqstore.Base
}

func (s *cycleStore) Create(ctx context.Context, in forecast.Cycle) error {
	if err := in.Validate(); err != nil {
		return err
	}

	err := s.base.Create(ctx, CycleEntity, in.ID, cycleToModel(in))
	if err != nil {
		// The unique index on (tenant_id, model, issued_at) is the authority
		// rather than a read-then-write check, which a concurrent backfill and
		// poll would both pass.
		if errors.Is(err, bserr.ErrConflict) {
			return fmt.Errorf(
				"%w: %s already has a cycle issued at %s",
				bserr.ErrConflict, in.Model,
				in.IssuedAt.UTC().Format(time.RFC3339))
		}

		return err
	}

	return nil
}

func (s *cycleStore) Get(ctx context.Context, want id.ID) (forecast.Cycle, error) {
	var m cycleModel

	if err := s.base.Get(ctx, CycleEntity, want, &m); err != nil {
		return forecast.Cycle{}, err
	}

	return cycleFromModel(m)
}

func (s *cycleStore) ForIssuance(
	ctx context.Context, model forecast.Model, issuedAt time.Time,
) (forecast.Cycle, error) {
	var rows []cycleModel

	// Raw SQL rather than the structured list, because the predicate is an
	// equality on a timestamp and the structured filter would compare it as
	// whatever the driver renders — the UTC normalisation above is only half
	// the guarantee without a bind parameter carrying the instant.
	if err := s.base.Query(ctx, &rows,
		`SELECT * FROM `+CycleTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND model = $1
		   AND issued_at = $2
		 LIMIT 1`, string(model), issuedAt.UTC()); err != nil {
		return forecast.Cycle{}, err
	}

	if len(rows) == 0 {
		return forecast.Cycle{}, fmt.Errorf(
			"%w: no %s cycle issued at %s", bserr.ErrNotFound, model,
			issuedAt.UTC().Format(time.RFC3339))
	}

	return cycleFromModel(rows[0])
}

func (s *cycleStore) SetStatus(
	ctx context.Context, want id.ID, status forecast.CycleStatus,
) error {
	if !status.Valid() {
		return fmt.Errorf("%w: %q is not a cycle status", bserr.ErrInvalid, status)
	}

	// Read-modify-write rather than a targeted UPDATE, because the command path
	// replaces the aggregate: a partial payload would blank every column it
	// omits, and the outbox event would carry that blanked image downstream.
	existing, err := s.Get(ctx, want)
	if err != nil {
		return err
	}

	existing.Status = status

	return s.base.Update(ctx, CycleEntity, want, cycleToModel(existing), nil)
}

func (s *cycleStore) List(
	ctx context.Context, f store.CycleFilter, p pkgstore.Page,
) ([]forecast.Cycle, error) {
	p = p.Normalize()

	q := query.ListQuery{Limit: p.Limit}

	if f.Model != "" {
		q.Where = append(q.Where, query.Eq("model", string(f.Model)))
	}

	if f.Status != "" {
		q.Where = append(q.Where, query.Eq("status", string(f.Status)))
	}

	// Newest issuance first: the question asked of this list is almost always
	// "what is the most recent run". The identifier breaks ties so two models
	// issued at one instant come back in a fixed order rather than whichever
	// the planner chose.
	q.OrderBy = "issued_at DESC, id ASC"

	var rows []cycleModel

	if err := s.base.Fabric().Relational().List(
		s.base.Scoped(ctx), CycleEntity, q, &rows,
	); err != nil {
		return nil, fabriqstore.Translate(err)
	}

	out := make([]forecast.Cycle, 0, len(rows))

	for _, row := range rows {
		got, err := cycleFromModel(row)
		if err != nil {
			return nil, err
		}

		out = append(out, got)
	}

	return out, nil
}
