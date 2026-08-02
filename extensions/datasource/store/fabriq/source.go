package fabriq

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/fabriq/core/query"

	"github.com/xraph/blackstart/extensions/datasource/source"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/store"
	"github.com/xraph/blackstart/pkg/store/fabriqstore"
)

type sourceStore struct {
	base *fabriqstore.Base
}

func (s *sourceStore) Get(ctx context.Context, want id.ID) (source.Source, error) {
	var m sourceModel

	if err := s.base.Get(ctx, SourceEntity, want, &m); err != nil {
		if errors.Is(err, bserr.ErrNotFound) {
			return source.Source{}, fmt.Errorf("%w: %s", source.ErrNotFound, want)
		}

		return source.Source{}, err
	}

	return sourceFromModel(m)
}

func (s *sourceStore) GetBySlug(ctx context.Context, slug string) (source.Source, error) {
	got, err := s.list(ctx, source.Filter{Slug: slug}, store.Page{Limit: 1})
	if err != nil {
		return source.Source{}, err
	}

	if len(got) == 0 {
		return source.Source{}, fmt.Errorf("%w: slug %q", source.ErrNotFound, slug)
	}

	return got[0], nil
}

func (s *sourceStore) List(
	ctx context.Context, f source.Filter, p store.Page,
) ([]source.Source, error) {
	return s.list(ctx, f, p)
}

func (s *sourceStore) list(
	ctx context.Context, f source.Filter, p store.Page,
) ([]source.Source, error) {
	p = p.Normalize()

	q := query.ListQuery{Limit: p.Limit}

	if f.Kind != "" {
		q.Where = append(q.Where, query.Eq("kind", string(f.Kind)))
	}

	if f.Slug != "" {
		q.Where = append(q.Where, query.Eq("slug", f.Slug))
	}

	// Keyset pagination over the identifier, which sorts chronologically. An
	// offset would re-scan everything it skipped and would drop or repeat rows
	// when the set changes mid-walk.
	if !p.IsFirst() {
		q.Where = append(q.Where, query.Gt("id", p.After.String()))
	}

	q.OrderBy = "id ASC"

	var models []sourceModel

	if err := s.base.Fabric().Relational().List(
		s.base.Scoped(ctx), SourceEntity, q, &models,
	); err != nil {
		return nil, fabriqstore.Translate(err)
	}

	out := make([]source.Source, 0, len(models))

	for _, m := range models {
		got, err := sourceFromModel(m)
		if err != nil {
			return nil, fmt.Errorf("datasource: decoding source %s: %w", m.ID, err)
		}

		out = append(out, got)
	}

	return out, nil
}

func (s *sourceStore) Create(ctx context.Context, in source.Source) error {
	if err := in.Validate(); err != nil {
		return err
	}

	err := s.base.Create(ctx, SourceEntity, in.ID, sourceToModel(in))
	if err != nil {
		// The unique index on (tenant_id, slug) is the authority here rather
		// than a read-then-write check, which two concurrent creates would
		// both pass.
		if errors.Is(err, bserr.ErrConflict) {
			return fmt.Errorf("%w: %q", source.ErrSlugTaken, in.Slug)
		}

		return err
	}

	return nil
}

func (s *sourceStore) Update(ctx context.Context, in source.Source) error {
	if err := in.Validate(); err != nil {
		return err
	}

	err := s.base.Update(ctx, SourceEntity, in.ID, sourceToModel(in), nil)
	if err != nil {
		if errors.Is(err, bserr.ErrNotFound) {
			return fmt.Errorf("%w: %s", source.ErrNotFound, in.ID)
		}

		if errors.Is(err, bserr.ErrConflict) {
			return fmt.Errorf("%w: %q", source.ErrSlugTaken, in.Slug)
		}

		return err
	}

	return nil
}
