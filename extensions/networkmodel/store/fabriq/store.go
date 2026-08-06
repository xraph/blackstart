package fabriq

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/xraph/fabriq"

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

// Branches returns the branch store.
func (s *Store) Branches() model.BranchStore { return (*branches)(s) }

// Placements returns the placement store.
func (s *Store) Placements() model.PlacementStore { return (*placements)(s) }

// Buses returns the bus store.
func (s *Store) Buses() model.BusStore { return (*buses)(s) }

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

// SetBlobRef records where a model's canonical encoding is stored.
func (s *models) SetBlobRef(
	ctx context.Context, want id.ID, reference string,
) (model.NetworkModel, error) {
	found, err := s.Get(ctx, want)
	if err != nil {
		return model.NetworkModel{}, err
	}

	found.BlobRef = reference

	// Through the command path like every other write, so the change appears
	// in the outbox and the audit trail. A direct UPDATE would be simpler and
	// would make the one mutation this store allows the one nothing records.
	if err := s.base.Update(ctx, ModelEntity, want, fromDomain(found), nil); err != nil {
		return model.NetworkModel{}, err
	}

	return found, nil
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

// Put stores the canonical bytes in Fabriq's content-addressable byte plane.
//
// Not a table of our own. Fabriq's blob plane is trove-backed CAS: identical
// bytes are stored once and ref-counted, and the catalog row it writes goes
// through the command path, so the blob gets the same tenancy, outbox and
// audit treatment as every other write. The hand-rolled BYTEA column this
// replaced had none of that, and kept megabytes of encoding in a row that a
// listing had to step over.
//
// The returned reference is checked against the hash we computed ourselves.
// Trove reports "sha256:<hex>" and the canonical hash is the same digest of
// the same bytes, so a mismatch means something re-encoded them between the
// hash and the write — which would leave a model naming an encoding that is
// not the one it was hashed from.
func (s *blobs) Put(ctx context.Context, body []byte) (string, error) {
	ref, err := s.base.Fabric().PutBlob(s.base.Scoped(ctx), bytes.NewReader(body),
		fabriq.PutBlobOpts{ContentType: canonicalContentType})
	if err != nil {
		return "", fmt.Errorf("networkmodel: storing the canonical encoding: %w",
			fabriqstore.Translate(err))
	}

	sum := sha256.Sum256(body)

	if want := "sha256:" + hex.EncodeToString(sum[:]); ref.Hash != want {
		return "", fmt.Errorf(
			"%w: the byte plane stored %s but these bytes hash to %s; something "+
				"altered them between hashing and writing, and the model would "+
				"name an encoding it was not hashed from",
			bserr.ErrConflict, ref.Hash, want)
	}

	return ref.ID, nil
}

// Get reads the bytes a reference names.
func (s *blobs) Get(ctx context.Context, reference string) ([]byte, error) {
	reader, _, err := s.base.Fabric().GetBlob(s.base.Scoped(ctx), reference)
	if err != nil {
		return nil, fmt.Errorf("networkmodel: reading the canonical encoding: %w",
			fabriqstore.Translate(err))
	}
	defer func() { _ = reader.Close() }()

	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("networkmodel: reading the canonical encoding: %w", err)
	}

	return body, nil
}

// canonicalContentType names what these bytes are, so an operator listing the
// object store sees a format rather than an opaque blob.
const canonicalContentType = "application/vnd.blackstart.canonical"
