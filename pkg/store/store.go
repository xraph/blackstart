// Package store holds the conventions every extension's storage layer shares.
//
// There is no StoreFactory type here, despite the project brief naming one.
// No such type exists anywhere in the substrate — it is a convention, not an
// API, and the convention is the composite store:
//
//	// extensions/topology/bus/store.go
//	type Store interface {
//	    Get(ctx context.Context, id id.ID) (Bus, error)
//	    List(ctx context.Context, f Filter, p Page) ([]Bus, error)
//	}
//
//	// extensions/topology/store/store.go
//	type Store interface {
//	    store.Lifecycle
//	    Buses() bus.Store
//	    Branches() branch.Store
//	}
//
// Each domain package owns an interface named Store; the composite gathers
// them and adds the lifecycle. Implementations live under store/postgres and
// store/memory, the latter doubling as the test double.
package store

import (
	"context"

	"github.com/xraph/blackstart/pkg/bitemporal"
	"github.com/xraph/blackstart/pkg/id"
)

// Lifecycle is what every composite store provides beyond its domain
// accessors.
type Lifecycle interface {
	// Migrate brings the schema up to date. It is a discrete deploy step, not
	// something a running application does at startup — Fabriq's own
	// documentation is explicit about that, and a migration racing N replicas
	// coming up is a bad way to learn it.
	Migrate(ctx context.Context) error

	// Ping reports whether the backing store is reachable. Wired to the host's
	// health endpoint.
	Ping(ctx context.Context) error

	// Close releases resources the store owns. A store given a borrowed
	// handle — which is the normal case, since the grove connection is owned
	// by the host — closes nothing.
	Close() error
}

// Page requests a bounded slice of a result set.
//
// Pagination is keyset, not offset. Offset pagination re-scans everything it
// skips, which turns a walk through two thousand buses into a quadratic one,
// and it silently drops or repeats rows when the underlying set changes
// mid-walk. Keyset pagination has neither problem.
//
// The cursor is an identifier because identifiers here sort chronologically —
// the TypeID suffix is a UUIDv7 — so "everything after this one" is both a
// stable cursor and a meaningful order without a separate sort column.
type Page struct {
	// Limit caps the number of rows returned. Zero means DefaultPageLimit.
	Limit int

	// After returns rows ordered after this identifier. Nil starts at the
	// beginning.
	After id.ID
}

// Page size bounds.
const (
	DefaultPageLimit = 100
	MaxPageLimit     = 1000
)

// Normalize clamps the limit into range and returns the result.
//
// Clamping rather than rejecting is deliberate for the upper bound: a caller
// asking for ten thousand rows gets a thousand and a cursor, which is more
// useful than an error telling them to ask again differently.
func (p Page) Normalize() Page {
	switch {
	case p.Limit <= 0:
		p.Limit = DefaultPageLimit
	case p.Limit > MaxPageLimit:
		p.Limit = MaxPageLimit
	}

	return p
}

// IsFirst reports whether this requests the first page.
func (p Page) IsFirst() bool { return p.After.IsZero() }

// Storable is satisfied by any composite store. It exists so that the host can
// discover stores generically — collecting them for health checks and for the
// migration step — without knowing their concrete types.
type Storable interface {
	Lifecycle

	// StoreName identifies the store in health output and migration logs.
	StoreName() string
}

// Temporal is satisfied by a store whose entities carry both time axes.
//
// The temporal context travels in context.Context rather than in method
// signatures, so this interface does not add AsOf parameters. What it declares
// is that the store *honors* that context — that its reads apply the
// bitemporal predicate rather than ignoring it.
//
// That distinction matters at review time. A store that is Temporal and does
// not honor the context is a bug the interface names; a store that is silently
// non-temporal is a bug nobody can see.
type Temporal interface {
	// TemporalEntities names the entities this store versions, for the
	// startup assertion that every one of them has the four interval columns.
	TemporalEntities() []string

	// AsOfSupported reports the earliest knowledge time this store can answer
	// for. Reads before it are outside the retained history and must fail
	// loudly rather than returning the oldest row that happens to exist —
	// which would look like a valid answer and silently corrupt a backtest.
	AsOfSupported() bitemporal.Instant
}

// Only these two capability interfaces exist so far, and both have real
// implementers coming in this phase. Spatial arrives with the geo extension in
// Phase 1, when there is geometry to describe.
//
// The brief lists Queryable, Aggregatable and Streamable as well. They are
// deliberately absent: nothing consumes them yet, and an interface nothing
// implements and nothing discovers by is worse than no interface at all —
// it looks like a contract while constraining nothing.
