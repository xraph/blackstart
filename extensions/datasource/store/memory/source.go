package memory

import (
	"context"
	"fmt"
	"slices"

	"github.com/xraph/blackstart/extensions/datasource/source"
	"github.com/xraph/blackstart/pkg/bitemporal"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/store"
)

// sourceStore is a view over Store, so the two domain stores share one lock
// and one clock without either holding a back-pointer.
type sourceStore Store

func (s *sourceStore) base() *Store { return (*Store)(s) }

func (s *sourceStore) Get(ctx context.Context, want id.ID) (source.Source, error) {
	b := s.base()

	b.mu.RLock()
	defer b.mu.RUnlock()

	got, ok := resolve(b.sources[want], bitemporal.Require(ctx))
	if !ok {
		return source.Source{}, fmt.Errorf("%w: %s", source.ErrNotFound, want)
	}

	return got, nil
}

func (s *sourceStore) GetBySlug(ctx context.Context, slug string) (source.Source, error) {
	b := s.base()

	b.mu.RLock()
	defer b.mu.RUnlock()

	asOf := bitemporal.Require(ctx)

	for _, versions := range b.sources {
		if got, ok := resolve(versions, asOf); ok && got.Slug == slug {
			return got, nil
		}
	}

	return source.Source{}, fmt.Errorf("%w: slug %q", source.ErrNotFound, slug)
}

func (s *sourceStore) List(
	ctx context.Context, f source.Filter, p store.Page,
) ([]source.Source, error) {
	b := s.base()

	b.mu.RLock()
	defer b.mu.RUnlock()

	asOf := bitemporal.Require(ctx)

	var matched []source.Source

	for _, versions := range b.sources {
		got, ok := resolve(versions, asOf)
		if !ok {
			continue
		}

		if f.Kind != "" && got.Kind != f.Kind {
			continue
		}

		if f.Slug != "" && got.Slug != f.Slug {
			continue
		}

		matched = append(matched, got)
	}

	// Identifier order, so pagination is stable and matches what the SQL store
	// returns. Map iteration order would make both untrue.
	slices.SortFunc(matched, func(a, c source.Source) int {
		return cmpID(a.ID, c.ID)
	})

	keys := make([]id.ID, len(matched))
	for i, m := range matched {
		keys[i] = m.ID
	}

	return paginate(matched, keys, p), nil
}

func (s *sourceStore) Create(ctx context.Context, in source.Source) error {
	if err := in.Validate(); err != nil {
		return err
	}

	b := s.base()

	b.mu.Lock()
	defer b.mu.Unlock()

	if _, exists := b.sources[in.ID]; exists {
		return fmt.Errorf("%w: %s", source.ErrAlreadyExists, in.ID)
	}

	asOf := bitemporal.Require(ctx)

	for _, versions := range b.sources {
		if got, ok := resolve(versions, asOf); ok && got.Slug == in.Slug {
			return fmt.Errorf("%w: %q", source.ErrSlugTaken, in.Slug)
		}
	}

	rec, err := bitemporal.Open(asOf.ValidAt, b.now())
	if err != nil {
		return err
	}

	b.sources[in.ID] = []version[source.Source]{{value: in, record: rec}}

	return nil
}

func (s *sourceStore) Update(ctx context.Context, in source.Source) error {
	if err := in.Validate(); err != nil {
		return err
	}

	b := s.base()

	b.mu.Lock()
	defer b.mu.Unlock()

	versions, exists := b.sources[in.ID]
	if !exists {
		return fmt.Errorf("%w: %s", source.ErrNotFound, in.ID)
	}

	prev, idx, ok := current(versions)
	if !ok {
		return errNoCurrentVersion
	}

	// A slug is a public handle; moving one to a different source silently
	// would redirect every command-line reference to it.
	asOf := bitemporal.Require(ctx)

	for otherID, otherVersions := range b.sources {
		if otherID == in.ID {
			continue
		}

		if got, ok := resolve(otherVersions, asOf); ok && got.Slug == in.Slug {
			return fmt.Errorf("%w: %q", source.ErrSlugTaken, in.Slug)
		}
	}

	archive, next, err := bitemporal.Correct(prev.record, b.now())
	if err != nil {
		return err
	}

	versions[idx] = version[source.Source]{value: prev.value, record: archive}
	b.sources[in.ID] = append(versions, version[source.Source]{value: in, record: next})

	return nil
}

func cmpID(a, b id.ID) int {
	switch {
	case a.String() < b.String():
		return -1
	case a.String() > b.String():
		return 1
	default:
		return 0
	}
}
