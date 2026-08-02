// Package datasource is the public data source registry.
//
// It is the extension that proves the seven-stage contract end to end, and it
// is not a toy: the registry it holds is the one the project's public-data
// guarantee depends on. Every dataset blackstart consumes must trace to an
// entry here, and a provenance record points at one.
package datasource

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/blackstart/extensions/datasource/feed"
	dsid "github.com/xraph/blackstart/extensions/datasource/id"
	"github.com/xraph/blackstart/extensions/datasource/plugin"
	"github.com/xraph/blackstart/extensions/datasource/source"
	"github.com/xraph/blackstart/extensions/datasource/store"
	"github.com/xraph/blackstart/pkg/hooks"
	"github.com/xraph/blackstart/pkg/id"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

// Engine holds the extension's business logic.
//
// It has no transport concerns and no storage details: it takes a store
// interface, so it can be exercised against the in-memory implementation with
// no database at all, and the HTTP layer sits above it rather than inside it.
type Engine struct {
	store   store.Store
	plugins *plugin.Manager
	bus     *hooks.Bus
}

// NewEngine builds the engine.
//
// The bus may be nil — a CLI invocation has no interest in domain events — and
// a nil manager means no subscribers, which is the normal case in tests.
func NewEngine(s store.Store, plugins *plugin.Manager, bus *hooks.Bus) (*Engine, error) {
	if s == nil {
		return nil, errors.New("datasource: engine needs a store")
	}

	if plugins == nil {
		plugins = plugin.NewManager(nil)
	}

	return &Engine{store: s, plugins: plugins, bus: bus}, nil
}

// CreateSource records a new data source.
//
// The identifier is minted here when the caller did not supply one, so an API
// client never has to. It is always set before the store sees it: Fabriq mints
// a bare ULID for an empty aggregate id, which would lose the type prefix the
// whole platform reads off its identifiers.
func (e *Engine) CreateSource(ctx context.Context, in source.Source) (source.Source, error) {
	if in.ID.IsZero() {
		in.ID = dsid.NewSource()
	}

	if err := e.store.Sources().Create(ctx, in); err != nil {
		return source.Source{}, err
	}

	e.plugins.EmitSourceCreated(ctx, in)
	e.emit(ctx, "datasource.source.created", hooks.OpCreate, in.ID, in)

	return in, nil
}

// UpdateSource records a change to a source.
//
// It reads the prior image first so subscribers receive both. The interesting
// question about a source change is almost always what changed — a license
// amendment matters and a typo fix does not — and a subscriber that had to
// re-read to find out would race the next write.
func (e *Engine) UpdateSource(ctx context.Context, in source.Source) (source.Source, error) {
	before, err := e.store.Sources().Get(ctx, in.ID)
	if err != nil {
		return source.Source{}, err
	}

	if err := e.store.Sources().Update(ctx, in); err != nil {
		return source.Source{}, err
	}

	e.plugins.EmitSourceUpdated(ctx, before, in)
	e.emit(ctx, "datasource.source.updated", hooks.OpUpdate, in.ID, in)

	return in, nil
}

// GetSource loads one source.
func (e *Engine) GetSource(ctx context.Context, want id.ID) (source.Source, error) {
	return e.store.Sources().Get(ctx, want)
}

// GetSourceBySlug loads one source by its command-line handle.
func (e *Engine) GetSourceBySlug(ctx context.Context, slug string) (source.Source, error) {
	return e.store.Sources().GetBySlug(ctx, slug)
}

// ListSources pages through the registry.
func (e *Engine) ListSources(
	ctx context.Context, f source.Filter, p pkgstore.Page,
) ([]source.Source, error) {
	return e.store.Sources().List(ctx, f, p)
}

// CreateFeed records a retrievable resource within a source.
//
// The source is resolved first, so a feed cannot be attached to something that
// does not exist. The store's foreign key would not catch it: the column is
// plain text, because the identifier carries its own type and a database-level
// reference would force both tables into one migration order for no gain.
func (e *Engine) CreateFeed(ctx context.Context, in feed.Feed) (feed.Feed, error) {
	if in.ID.IsZero() {
		in.ID = dsid.NewFeed()
	}

	if _, err := e.store.Sources().Get(ctx, in.SourceID); err != nil {
		return feed.Feed{}, fmt.Errorf(
			"datasource: feed %q names source %s: %w", in.Slug, in.SourceID, err)
	}

	if err := e.store.Feeds().Create(ctx, in); err != nil {
		return feed.Feed{}, err
	}

	e.plugins.EmitFeedCreated(ctx, in)
	e.emit(ctx, "datasource.feed.created", hooks.OpCreate, in.ID, in)

	return in, nil
}

// UpdateFeed records a change to a feed.
func (e *Engine) UpdateFeed(ctx context.Context, in feed.Feed) (feed.Feed, error) {
	before, err := e.store.Feeds().Get(ctx, in.ID)
	if err != nil {
		return feed.Feed{}, err
	}

	if err := e.store.Feeds().Update(ctx, in); err != nil {
		return feed.Feed{}, err
	}

	e.plugins.EmitFeedUpdated(ctx, before, in)
	e.emit(ctx, "datasource.feed.updated", hooks.OpUpdate, in.ID, in)

	return in, nil
}

// GetFeed loads one feed.
func (e *Engine) GetFeed(ctx context.Context, want id.ID) (feed.Feed, error) {
	return e.store.Feeds().Get(ctx, want)
}

// ListFeeds pages through the feeds of a source, or of all sources.
func (e *Engine) ListFeeds(
	ctx context.Context, f feed.Filter, p pkgstore.Page,
) ([]feed.Feed, error) {
	return e.store.Feeds().List(ctx, f, p)
}

// Register wires a hook subscriber.
func (e *Engine) Register(subscriber any) { e.plugins.Register(subscriber) }

// Store exposes the composite for lifecycle wiring — health checks and the
// migration step. It is not a general escape hatch, and callers reaching
// through it to write are bypassing the validation and the hooks above.
func (e *Engine) Store() store.Store { return e.store }

// emit publishes a domain event on the shared bus, when there is one.
//
// This is the out-of-transaction, best-effort path. Audit entries for these
// writes are produced separately by a Fabriq lifecycle hook inside the write
// transaction — conflating the two is the mistake pkg/hooks warns about.
func (e *Engine) emit(ctx context.Context, name string, op hooks.Op, entityID id.ID, payload any) {
	if e.bus == nil {
		return
	}

	e.bus.Emit(ctx, hooks.Event{
		Name:      name,
		Extension: "datasource",
		Entity:    string(entityID.Prefix()),
		EntityID:  entityID.String(),
		Op:        op,
		Payload:   payload,
	})
}
