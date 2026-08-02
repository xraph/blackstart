package fabriq

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/fabriq/core/query"

	"github.com/xraph/blackstart/extensions/datasource/feed"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/store"
	"github.com/xraph/blackstart/pkg/store/fabriqstore"
)

type feedStore struct {
	base *fabriqstore.Base
}

func (s *feedStore) Get(ctx context.Context, want id.ID) (feed.Feed, error) {
	var m feedModel

	if err := s.base.Get(ctx, FeedEntity, want, &m); err != nil {
		if errors.Is(err, bserr.ErrNotFound) {
			return feed.Feed{}, fmt.Errorf("%w: %s", feed.ErrNotFound, want)
		}

		return feed.Feed{}, err
	}

	return feedFromModel(m)
}

func (s *feedStore) List(
	ctx context.Context, f feed.Filter, p store.Page,
) ([]feed.Feed, error) {
	p = p.Normalize()

	q := query.ListQuery{Limit: p.Limit, OrderBy: "id ASC"}

	if !f.SourceID.IsZero() {
		q.Where = append(q.Where, query.Eq("source_id", f.SourceID.String()))
	}

	if f.Format != "" {
		q.Where = append(q.Where, query.Eq("format", string(f.Format)))
	}

	if !p.IsFirst() {
		q.Where = append(q.Where, query.Gt("id", p.After.String()))
	}

	var models []feedModel

	if err := s.base.Fabric().Relational().List(
		s.base.Scoped(ctx), FeedEntity, q, &models,
	); err != nil {
		return nil, fabriqstore.Translate(err)
	}

	out := make([]feed.Feed, 0, len(models))

	for _, m := range models {
		got, err := feedFromModel(m)
		if err != nil {
			return nil, fmt.Errorf("datasource: decoding feed %s: %w", m.ID, err)
		}

		out = append(out, got)
	}

	return out, nil
}

func (s *feedStore) Create(ctx context.Context, in feed.Feed) error {
	if err := in.Validate(); err != nil {
		return err
	}

	err := s.base.Create(ctx, FeedEntity, in.ID, feedToModel(in))
	if err != nil {
		if errors.Is(err, bserr.ErrConflict) {
			return fmt.Errorf("%w: %q under source %s",
				feed.ErrSlugTaken, in.Slug, in.SourceID)
		}

		return err
	}

	return nil
}

func (s *feedStore) Update(ctx context.Context, in feed.Feed) error {
	if err := in.Validate(); err != nil {
		return err
	}

	err := s.base.Update(ctx, FeedEntity, in.ID, feedToModel(in), nil)
	if err != nil {
		if errors.Is(err, bserr.ErrNotFound) {
			return fmt.Errorf("%w: %s", feed.ErrNotFound, in.ID)
		}

		if errors.Is(err, bserr.ErrConflict) {
			return fmt.Errorf("%w: %q", feed.ErrSlugTaken, in.Slug)
		}

		return err
	}

	return nil
}
