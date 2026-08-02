package fabriqstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/xraph/fabriq"
	"github.com/xraph/fabriq/core/command"
	"github.com/xraph/fabriq/core/query"
	"github.com/xraph/fabriq/core/tenant"

	"github.com/xraph/blackstart/pkg/bitemporal"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Base is embedded by every extension's Fabriq-backed store.
//
// It owns three things extensions should not each re-solve: stamping the
// tenant onto every context, translating Fabriq's errors into the shared
// kinds, and composing the bitemporal predicate into reads.
type Base struct {
	fab    *fabriq.Fabriq
	tenant string
}

// New builds a base over a Fabriq facade and the tenant every call is scoped
// to.
//
// Blackstart is single-tenant: one constant identifier, stamped in one place.
// Fabriq has no default-tenant mode by design — a missing tenant is a hard
// failure at every entry point — so the identifier is supplied here once
// rather than being threaded from request state that does not exist.
func New(fab *fabriq.Fabriq, tenantID string) (*Base, error) {
	if fab == nil {
		return nil, errors.New("fabriqstore: no fabriq facade")
	}

	if !tenant.Valid(tenantID) {
		return nil, fmt.Errorf(
			"fabriqstore: tenant id %q is invalid; fabriq derives graph names, "+
				"index names and stream keys from it, so it must match its "+
				"character rules", tenantID)
	}

	return &Base{fab: fab, tenant: tenantID}, nil
}

// Fabric exposes the underlying facade for the reads this base does not wrap.
// Prefer the wrapped helpers: a caller using this directly is responsible for
// stamping the tenant and translating errors itself.
func (b *Base) Fabric() *fabriq.Fabriq { return b.fab }

// Tenant returns the tenant identifier every call is scoped to.
func (b *Base) Tenant() string { return b.tenant }

// Scoped stamps the tenant onto a context.
//
// Every method below calls it, and any code reaching Fabriq by another route
// must too. Forgetting produces ErrNoTenant, which Translate deliberately
// reports as an internal error rather than a client one — see errors.go.
func (b *Base) Scoped(ctx context.Context) context.Context {
	return tenant.MustWithTenant(ctx, b.tenant)
}

// Create writes a new aggregate under a caller-supplied identifier.
//
// The identifier is always supplied. Fabriq mints a ULID when AggID is empty,
// which would bypass the type-prefixed identifiers the rest of the platform
// depends on — a bus whose primary key is a bare ULID is indistinguishable
// from a substation's.
func (b *Base) Create(ctx context.Context, entity string, aggID id.ID, payload any) error {
	if aggID.IsZero() {
		return fmt.Errorf(
			"%w: creating a %s requires an explicit identifier; fabriq would "+
				"otherwise mint a bare ULID and the entity type would be lost",
			bserr.ErrInvalid, entity)
	}

	_, err := b.fab.Exec(b.Scoped(ctx), command.Command{
		Entity:  entity,
		Op:      command.OpCreate,
		AggID:   aggID.String(),
		Payload: payload,
	})

	return Translate(err)
}

// Update replaces an aggregate. When expectedVersion is non-nil the write
// fails on a concurrent modification rather than overwriting it.
func (b *Base) Update(
	ctx context.Context, entity string, aggID id.ID, payload any, expectedVersion *int64,
) error {
	_, err := b.fab.Exec(b.Scoped(ctx), command.Command{
		Entity:          entity,
		Op:              command.OpUpdate,
		AggID:           aggID.String(),
		Payload:         payload,
		ExpectedVersion: expectedVersion,
	})

	return Translate(err)
}

// Delete removes an aggregate. The deletion is still a versioned event, so
// projections and the outbox see it.
func (b *Base) Delete(ctx context.Context, entity string, aggID id.ID) error {
	_, err := b.fab.Exec(b.Scoped(ctx), command.Command{
		Entity: entity,
		Op:     command.OpDelete,
		AggID:  aggID.String(),
	})

	return Translate(err)
}

// Batch executes several commands in one transaction.
//
// This is how a bitemporal supersession is written: archiving the prior image
// and inserting the replacement are one atomic step. Splitting them leaves a
// window in which the entity has no current version, and a crash inside it
// loses the fact.
func (b *Base) Batch(ctx context.Context, cmds []command.Command) error {
	if len(cmds) == 0 {
		return nil
	}

	_, err := b.fab.ExecBatch(b.Scoped(ctx), cmds)

	return Translate(err)
}

// Get loads one aggregate into a model pointer.
func (b *Base) Get(ctx context.Context, entity string, aggID id.ID, into any) error {
	return Translate(b.fab.Relational().Get(b.Scoped(ctx), entity, aggID.String(), into))
}

// QueryAsOf runs a read with the bitemporal predicate applied.
//
// selectSQL is everything up to the WHERE — "SELECT id, name FROM
// blackstart_bus AS b" — and alias is the table alias used inside it, or "" if
// there is none. extraWhere is the caller's own condition, using placeholders
// $1..$n matching args; the temporal placeholders follow.
//
// Composing it here rather than at each call site is the point. Hand-written
// bitemporal predicates drift into the "OR valid_to IS NULL" variant and the
// forgot-the-knowledge-axis variant, both of which return plausible rows.
func (b *Base) QueryAsOf(
	ctx context.Context, into any, selectSQL, alias, extraWhere string, args ...any,
) error {
	asOf := bitemporal.Require(ctx)

	pred, temporalArgs := bitemporal.Predicate(alias, asOf, len(args)+1)

	var sql strings.Builder

	sql.WriteString(selectSQL)
	sql.WriteString(" WHERE ")
	sql.WriteString(pred)

	if extraWhere != "" {
		sql.WriteString(" AND (")
		sql.WriteString(extraWhere)
		sql.WriteString(")")
	}

	all := make([]any, 0, len(args)+len(temporalArgs))
	all = append(all, args...)
	all = append(all, temporalArgs...)

	return Translate(b.fab.Relational().Query(b.Scoped(ctx), into, sql.String(), all...))
}

// Ping reports whether the source of truth is reachable.
func (b *Base) Ping(ctx context.Context) error {
	var probe []struct {
		OK int `grove:"ok"`
	}

	return Translate(b.fab.Relational().Query(b.Scoped(ctx), &probe, "SELECT 1 AS ok"))
}

// Query runs a read with no temporal predicate.
//
// For entities that are not bitemporal, and only those. A network model is a
// snapshot, a provenance record states what happened, a solution records an
// answer — none of them is corrected, so none has a valid-time interval to
// filter on and QueryAsOf would compose a predicate against columns that do
// not exist.
//
// The distinction is worth stating because the failure is quiet in opposite
// directions. Using this on a bitemporal entity returns every version of every
// row, which reads as duplicates. Using QueryAsOf on one that is not fails at
// the database with a missing column, which is loud and therefore fine.
func (b *Base) Query(ctx context.Context, into any, sql string, args ...any) error {
	return Translate(b.fab.Relational().Query(b.Scoped(ctx), into, sql, args...))
}

// Spatial writes a geometry through Fabriq's PostGIS port.
//
// Direct-write rather than part of the aggregate's command, which is what the
// port is: geometry does not pass through the outbox. The consequence is that
// a crash between the row write and this one leaves an entity with no
// geometry, so every caller also stores the well-known text on the row itself
// — the geometry column is then a rebuildable index rather than the only copy.
//
// # Errors
//
// Returns a translated error. A geometry port that is not configured — plain
// Postgres with no PostGIS — reports so rather than silently storing nothing.
func (b *Base) Spatial(
	ctx context.Context, entity string, aggID id.ID, wkt string, srid int,
	meta map[string]any,
) error {
	spatial := b.fab.Spatial()
	if spatial == nil {
		return fmt.Errorf(
			"%w: no geometry port; the database has no PostGIS extension, so "+
				"%s geometry cannot be indexed", bserr.ErrUnavailable, entity)
	}

	if err := spatial.Upsert(b.Scoped(ctx), entity, aggID.String(),
		query.Geometry{WKT: wkt, SRID: srid}, meta); err != nil {
		return fmt.Errorf("writing %s geometry: %w", entity, Translate(err))
	}

	return nil
}
