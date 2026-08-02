// Package fabriq is the Fabriq-backed implementation of the datasource store.
//
// Models here are unexported and per-backend, with explicit mappers to and
// from the domain types. The domain structs carry no grove tags: keeping the
// persistence shape separate is what lets the column layout change — adding
// the bitemporal columns, say — without touching the type the rest of the
// platform passes around.
package fabriq

import (
	"github.com/xraph/grove"

	"github.com/xraph/blackstart/extensions/datasource/feed"
	"github.com/xraph/blackstart/extensions/datasource/source"
	"github.com/xraph/blackstart/pkg/id"
)

// Table names. The prefix is the extension name, matching the house
// convention and keeping a shared schema legible.
const (
	SourceTable = "blackstart_datasource_source"
	FeedTable   = "blackstart_datasource_feed"
)

// Entity names as registered with Fabriq. These are what a command.Command
// carries, and they are global to the registry.
const (
	SourceEntity = "datasource_source"
	FeedEntity   = "datasource_feed"
)

// sourceModel is the persistence shape of a source.
//
// The first three fields are Fabriq's structural contract: every entity must
// declare id, tenant_id and version, and the registry refuses a model that
// does not. They are stamped by the executor, so caller values are ignored.
type sourceModel struct {
	grove.BaseModel `grove:"table:blackstart_datasource_source"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	Slug                string `grove:"slug,notnull"`
	Name                string `grove:"name,notnull"`
	Kind                string `grove:"kind,notnull"`
	Provides            string `grove:"provides"`
	Homepage            string `grove:"homepage"`
	License             string `grove:"license,notnull"`
	RequiresAttribution bool   `grove:"requires_attribution,notnull"`
}

// feedModel is the persistence shape of a feed.
type feedModel struct {
	grove.BaseModel `grove:"table:blackstart_datasource_feed"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	// SourceID is the column Fabriq's edge spec names, and therefore the one
	// the graph projection follows.
	SourceID string `grove:"source_id,notnull"`

	Slug    string `grove:"slug,notnull"`
	Name    string `grove:"name,notnull"`
	URL     string `grove:"url,notnull"`
	Format  string `grove:"format,notnull"`
	Cadence string `grove:"cadence"`
}

func sourceToModel(s source.Source) *sourceModel {
	return &sourceModel{
		ID:                  s.ID.String(),
		Slug:                s.Slug,
		Name:                s.Name,
		Kind:                string(s.Kind),
		Provides:            s.Provides,
		Homepage:            s.Homepage,
		License:             s.License,
		RequiresAttribution: s.RequiresAttribution,
	}
}

func sourceFromModel(m sourceModel) (source.Source, error) {
	parsed, err := id.Parse(m.ID)
	if err != nil {
		return source.Source{}, err
	}

	return source.Source{
		ID:                  parsed,
		Slug:                m.Slug,
		Name:                m.Name,
		Kind:                source.Kind(m.Kind),
		Provides:            m.Provides,
		Homepage:            m.Homepage,
		License:             m.License,
		RequiresAttribution: m.RequiresAttribution,
	}, nil
}

func feedToModel(f feed.Feed) *feedModel {
	return &feedModel{
		ID:       f.ID.String(),
		SourceID: f.SourceID.String(),
		Slug:     f.Slug,
		Name:     f.Name,
		URL:      f.URL,
		Format:   string(f.Format),
		Cadence:  f.Cadence,
	}
}

func feedFromModel(m feedModel) (feed.Feed, error) {
	parsedID, err := id.Parse(m.ID)
	if err != nil {
		return feed.Feed{}, err
	}

	parsedSource, err := id.Parse(m.SourceID)
	if err != nil {
		return feed.Feed{}, err
	}

	return feed.Feed{
		ID:       parsedID,
		SourceID: parsedSource,
		Slug:     m.Slug,
		Name:     m.Name,
		URL:      m.URL,
		Format:   feed.Format(m.Format),
		Cadence:  m.Cadence,
	}, nil
}
