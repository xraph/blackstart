// Package plugin declares the hooks other components can implement to react
// to datasource changes.
//
// One interface per event, rather than one interface with many methods.
// A subscriber that cares only about source creation implements one method;
// with a single wide interface it would have to stub the rest, and every new
// event would break every existing subscriber.
//
// The manager resolves subscribers into per-event slices at registration, so
// emitting is a slice walk with no type assertions on the hot path.
package plugin

import (
	"context"

	"github.com/xraph/blackstart/extensions/datasource/feed"
	"github.com/xraph/blackstart/extensions/datasource/source"
)

// SourceCreated is implemented by subscribers that react to a new source.
type SourceCreated interface {
	OnSourceCreated(ctx context.Context, s source.Source) error
}

// SourceUpdated is implemented by subscribers that react to a change.
//
// It carries both images because the interesting question is almost always
// what changed — a license amendment matters, a typo in the name does not.
type SourceUpdated interface {
	OnSourceUpdated(ctx context.Context, before, after source.Source) error
}

// FeedCreated is implemented by subscribers that react to a new feed.
type FeedCreated interface {
	OnFeedCreated(ctx context.Context, f feed.Feed) error
}

// FeedUpdated is implemented by subscribers that react to a feed change.
type FeedUpdated interface {
	OnFeedUpdated(ctx context.Context, before, after feed.Feed) error
}
