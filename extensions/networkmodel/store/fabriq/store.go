package fabriq

import (
	"context"
	"fmt"

	"github.com/xraph/fabriq"

	nmid "github.com/xraph/blackstart/extensions/networkmodel/id"
	"github.com/xraph/blackstart/extensions/networkmodel/model"
	"github.com/xraph/blackstart/extensions/networkmodel/store"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
	"github.com/xraph/blackstart/pkg/store/fabriqstore"
)

// Store is the Fabriq-backed composite.
//
// Every write goes through fabriqstore.Base, which stamps the tenant, wraps
// the command and translates the error. Nothing here reaches Fabriq directly,
// so a breaking change in a v0.0.x API is a one-package fix rather than a
// sweep through every store in the platform.
type Store struct {
	base *fabriqstore.Base
}

// New builds the store over a Fabriq facade.
func New(fab *fabriq.Fabriq, tenant string) (*Store, error) {
	base, err := fabriqstore.New(fab, tenant)
	if err != nil {
		return nil, fmt.Errorf("networkmodel: building the store: %w", err)
	}

	return &Store{base: base}, nil
}

// StoreName identifies the store in health output and migration logs.
func (s *Store) StoreName() string { return "networkmodel/fabriq" }

// Migrate is a no-op here. Schema changes are a discrete deploy step run
// through the migration group in migrations.go, never something a store does
// on the way up — a migration racing N replicas starting is a bad way to learn
// why Fabriq's documentation insists on the separation.
func (s *Store) Migrate(context.Context) error { return nil }

// Ping reports whether the source of truth is reachable.
func (s *Store) Ping(ctx context.Context) error { return s.base.Ping(ctx) }

// Close releases nothing: the Fabriq facade is owned by the fabric extension.
func (s *Store) Close() error { return nil }

// Models returns the model store.
func (s *Store) Models() model.Store { return (*models)(s) }

// Blobs returns the blob store.
func (s *Store) Blobs() store.BlobStore { return (*blobs)(s) }

type models Store

func (s *models) Create(ctx context.Context, in model.NetworkModel) (model.NetworkModel, error) {
	if err := in.Validate(); err != nil {
		return model.NetworkModel{}, err
	}

	if err := s.base.Create(ctx, ModelEntity, in.ID, fromDomain(in)); err != nil {
		return model.NetworkModel{}, err
	}

	return in, nil
}

func (s *models) Get(ctx context.Context, want id.ID) (model.NetworkModel, error) {
	var row modelRow

	if err := s.base.Get(ctx, ModelEntity, want, &row); err != nil {
		return model.NetworkModel{}, err
	}

	return row.toDomain()
}

func (s *models) FindByHash(ctx context.Context, contentHash string) (model.NetworkModel, error) {
	var rows []modelRow

	// Ordered and limited in SQL rather than in Go, because two models can
	// share a hash — the same network imported twice under different names —
	// and the earliest is the one every existing result's hash already
	// resolves to.
	err := s.base.Query(ctx, &rows,
		`SELECT * FROM `+ModelTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND content_hash = $1
		 ORDER BY built_at ASC, id ASC
		 LIMIT 1`, contentHash)
	if err != nil {
		return model.NetworkModel{}, err
	}

	if len(rows) == 0 {
		return model.NetworkModel{}, fmt.Errorf(
			"%w: no network model with content hash %s", bserr.ErrNotFound, contentHash)
	}

	return rows[0].toDomain()
}

func (s *models) List(ctx context.Context, limit int) ([]model.NetworkModel, error) {
	if limit <= 0 {
		limit = defaultListLimit
	}

	var rows []modelRow

	err := s.base.Query(ctx, &rows,
		`SELECT * FROM `+ModelTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		 ORDER BY built_at DESC, id DESC
		 LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}

	out := make([]model.NetworkModel, 0, len(rows))

	for _, row := range rows {
		converted, err := row.toDomain()
		if err != nil {
			return nil, err
		}

		out = append(out, converted)
	}

	return out, nil
}

// defaultListLimit bounds an unbounded listing.
//
// A caller asking for "all models" on a busy installation would otherwise page
// the whole table into memory. The memory store returns everything because it
// holds everything anyway; here the bound is real.
const defaultListLimit = 100

type blobs Store

func (s *blobs) Put(ctx context.Context, contentHash string, body []byte) error {
	// Looked up before writing, so a re-put of identical bytes is an
	// idempotent no-op rather than a second copy of a megabyte. The unique
	// index on content_hash would reject the duplicate anyway; checking here
	// makes the common case succeed quietly instead of erroring.
	existing, err := s.Get(ctx, contentHash)
	if err == nil {
		if string(existing) == string(body) {
			return nil
		}

		return fmt.Errorf(
			"%w: content hash %s already holds different bytes; a blob is named "+
				"by the hash of its own content, so this is a caller passing the "+
				"wrong hash", bserr.ErrConflict, contentHash)
	}

	return s.base.Create(ctx, BlobEntity, nmid.NewBlob(), blobRow{
		ContentHash: contentHash,
		Body:        body,
		SizeBytes:   len(body),
	})
}

func (s *blobs) Get(ctx context.Context, contentHash string) ([]byte, error) {
	var rows []blobRow

	err := s.base.Query(ctx, &rows,
		`SELECT * FROM `+BlobTable+`
		 WHERE tenant_id = current_setting('app.tenant_id', true)
		   AND content_hash = $1
		 LIMIT 1`, contentHash)
	if err != nil {
		return nil, err
	}

	if len(rows) == 0 {
		return nil, fmt.Errorf(
			"%w: no blob for content hash %s", bserr.ErrNotFound, contentHash)
	}

	return rows[0].Body, nil
}
