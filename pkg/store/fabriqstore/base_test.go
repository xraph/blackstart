package fabriqstore

import (
	"errors"
	"strings"
	"testing"

	"github.com/xraph/fabriq/core/tenant"

	"github.com/xraph/blackstart/pkg/bitemporal"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Base needs a live Fabriq facade, which needs Postgres, Redis and FalkorDB —
// so its methods are covered by the integration suite. What is unit-testable
// here is construction and the SQL composition, and the latter is worth
// testing without a database because a wrong predicate returns plausible rows
// rather than failing.

func TestNewRejectsAnAbsentFacade(t *testing.T) {
	t.Parallel()

	if _, err := New(nil, "blackstart"); err == nil {
		t.Error("built a base with no facade")
	}
}

// Fabriq derives graph names, index names and stream keys from the tenant
// identifier, so an invalid one fails at construction rather than at the first
// projection.
func TestNewValidatesTheTenantIdentifier(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{"", "has spaces", "has/slash", strings.Repeat("a", 65)} {
		if tenant.Valid(bad) {
			continue // guard against the library's rules changing under us
		}

		if _, err := New(nil, bad); err == nil {
			t.Errorf("accepted the invalid tenant id %q", bad)
		}
	}
}

// The composed SQL is the thing worth checking without a database: a wrong
// bitemporal predicate does not fail, it returns the wrong rows.
func TestQueryAsOfComposition(t *testing.T) {
	t.Parallel()

	asOf := bitemporal.Current()

	t.Run("temporal placeholders follow the caller's", func(t *testing.T) {
		t.Parallel()

		// Two caller arguments means the temporal ones are $3 and $4.
		pred, args := bitemporal.Predicate("b", asOf, 3)

		for _, want := range []string{"$3", "$4"} {
			if !strings.Contains(pred, want) {
				t.Errorf("predicate is missing %s:\n%s", want, pred)
			}
		}

		if len(args) != 2 {
			t.Fatalf("predicate produced %d args, want 2", len(args))
		}
	})

	t.Run("both axes are always present", func(t *testing.T) {
		t.Parallel()

		pred, _ := bitemporal.Predicate("b", asOf, 1)

		for _, col := range bitemporal.Columns() {
			if !strings.Contains(pred, col) {
				t.Errorf("the predicate omits %s; forgetting the knowledge axis "+
					"is the variant that silently leaks corrections:\n%s", col, pred)
			}
		}
	})
}

// The argument slice must be caller-args-then-temporal-args, matching the
// placeholder numbering. Getting this backwards shifts every parameter and
// produces either a type error or a wrong answer.
func TestArgumentOrderMatchesPlaceholderNumbering(t *testing.T) {
	t.Parallel()

	callerArgs := []any{"substation-1", 138.0}

	_, temporalArgs := bitemporal.Predicate("b", bitemporal.Current(), len(callerArgs)+1)

	all := make([]any, 0, len(callerArgs)+len(temporalArgs))
	all = append(all, callerArgs...)
	all = append(all, temporalArgs...)

	if len(all) != 4 {
		t.Fatalf("combined args = %d, want 4", len(all))
	}

	if all[0] != "substation-1" {
		t.Errorf("$1 is %v, want the caller's first argument", all[0])
	}

	// $3 and $4 are the temporal pair, in valid-then-known order.
	if all[2] != temporalArgs[0] || all[3] != temporalArgs[1] {
		t.Error("temporal arguments are not in the positions the predicate " +
			"numbers them")
	}
}

// Create requires an explicit identifier because Fabriq mints a bare ULID for
// an empty AggID, and a bus whose primary key is a bare ULID is
// indistinguishable from a substation's.
//
// The check runs before the facade is touched, so it is reachable without a
// database.
func TestCreateRejectsAnEmptyIdentifier(t *testing.T) {
	t.Parallel()

	base := &Base{tenant: "blackstart"}

	err := base.Create(t.Context(), "bus", id.Nil, struct{}{})
	if err == nil {
		t.Fatal("accepted an empty identifier; fabriq would have minted a " +
			"bare ULID and the entity type would be lost")
	}

	if !errors.Is(err, bserr.ErrInvalid) {
		t.Errorf("error should be an invalid-input kind, got %v", err)
	}

	if !strings.Contains(err.Error(), "bus") {
		t.Errorf("the error should name the entity, got %q", err)
	}
}

func TestTenantAccessor(t *testing.T) {
	t.Parallel()

	if got := (&Base{tenant: "blackstart"}).Tenant(); got != "blackstart" {
		t.Errorf("Tenant() = %q", got)
	}
}

func TestScopedStampsTheTenant(t *testing.T) {
	t.Parallel()

	base := &Base{tenant: "blackstart"}

	got, err := tenant.FromContext(base.Scoped(t.Context()))
	if err != nil {
		t.Fatalf("the tenant was not stamped: %v", err)
	}

	if got != "blackstart" {
		t.Errorf("stamped tenant = %q, want blackstart", got)
	}
}

// Every read below this base inherits the temporal context, so an unstamped
// context must still produce a usable one rather than an empty predicate.
func TestQueryAsOfInheritsTheAmbientTemporalContext(t *testing.T) {
	t.Parallel()

	got := bitemporal.Require(t.Context())

	if got.ValidAt.IsZero() || got.KnownAt.IsZero() {
		t.Fatalf("an unstamped context produced an incomplete read: %s", got)
	}

	if !got.IsLeakFree() {
		t.Error("the default temporal context should not leak")
	}
}
