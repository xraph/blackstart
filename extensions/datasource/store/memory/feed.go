package memory

import (
	"context"
	"fmt"
	"slices"

	"github.com/xraph/blackstart/extensions/datasource/feed"
	"github.com/xraph/blackstart/pkg/bitemporal"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/store"
)

type feedStore Store

func (s *feedStore) base() *Store { return (*Store)(s) }

func (s *feedStore) Get(ctx context.Context, want id.ID) (feed.Feed, error) {
	b := s.base()

	b.mu.RLock()
	defer b.mu.RUnlock()

	got, ok := resolve(b.feeds[want], bitemporal.Require(ctx))
	if !ok {
		return feed.Feed{}, fmt.Errorf("%w: %s", feed.ErrNotFound, want)
	}

	return got, nil
}

func (s *feedStore) List(
	ctx context.Context, f feed.Filter, p store.Page,
) ([]feed.Feed, error) {
	b := s.base()

	b.mu.RLock()
	defer b.mu.RUnlock()

	asOf := bitemporal.Require(ctx)

	var matched []feed.Feed

	for _, versions := range b.feeds {
		got, ok := resolve(versions, asOf)
		if !ok {
			continue
		}

		if !f.SourceID.IsZero() && got.SourceID != f.SourceID {
			continue
		}

		if f.Format != "" && got.Format != f.Format {
			continue
		}

		matched = append(matched, got)
	}

	slices.SortFunc(matched, func(a, c feed.Feed) int { return cmpID(a.ID, c.ID) })

	keys := make([]id.ID, len(matched))
	for i, m := range matched {
		keys[i] = m.ID
	}

	return paginate(matched, keys, p), nil
}

func (s *feedStore) Create(ctx context.Context, in feed.Feed) error {
	if err := in.Validate(); err != nil {
		return err
	}

	b := s.base()

	b.mu.Lock()
	defer b.mu.Unlock()

	if _, exists := b.feeds[in.ID]; exists {
		return fmt.Errorf("%w: %s", feed.ErrSlugTaken, in.ID)
	}

	asOf := bitemporal.Require(ctx)

	// A feed slug is unique within its source, not globally: "hrrr" under NOAA
	// and "hrrr" under a mirror are different feeds with the same short name.
	if err := b.assertFeedSlugFree(asOf, in, id.Nil); err != nil {
		return err
	}

	rec, err := bitemporal.Open(asOf.ValidAt, b.now())
	if err != nil {
		return err
	}

	b.feeds[in.ID] = []version[feed.Feed]{{value: in, record: rec}}

	return nil
}

func (s *feedStore) Update(ctx context.Context, in feed.Feed) error {
	if err := in.Validate(); err != nil {
		return err
	}

	b := s.base()

	b.mu.Lock()
	defer b.mu.Unlock()

	versions, exists := b.feeds[in.ID]
	if !exists {
		return fmt.Errorf("%w: %s", feed.ErrNotFound, in.ID)
	}

	prev, idx, ok := current(versions)
	if !ok {
		return errNoCurrentVersion
	}

	asOf := bitemporal.Require(ctx)
	if err := b.assertFeedSlugFree(asOf, in, in.ID); err != nil {
		return err
	}

	archive, next, err := bitemporal.Correct(prev.record, b.now())
	if err != nil {
		return err
	}

	versions[idx] = version[feed.Feed]{value: prev.value, record: archive}
	b.feeds[in.ID] = append(versions, version[feed.Feed]{value: in, record: next})

	return nil
}

// assertFeedSlugFree checks slug uniqueness within a source, ignoring the feed
// being written. The caller holds the lock.
func (s *Store) assertFeedSlugFree(asOf bitemporal.AsOf, in feed.Feed, ignore id.ID) error {
	for otherID, versions := range s.feeds {
		if otherID == ignore {
			continue
		}

		got, ok := resolve(versions, asOf)
		if !ok {
			continue
		}

		if got.SourceID == in.SourceID && got.Slug == in.Slug {
			return fmt.Errorf("%w: %q under source %s",
				feed.ErrSlugTaken, in.Slug, in.SourceID)
		}
	}

	return nil
}
