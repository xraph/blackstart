package store

import (
	"context"
	"testing"

	"github.com/xraph/blackstart/pkg/bitemporal"
	"github.com/xraph/blackstart/pkg/id"
)

func TestPageNormalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		limit int
		want  int
	}{
		{"zero takes the default", 0, DefaultPageLimit},
		{"negative takes the default", -5, DefaultPageLimit},
		{"in range is untouched", 50, 50},
		{"at the maximum is untouched", MaxPageLimit, MaxPageLimit},
		{"over the maximum is clamped", 10000, MaxPageLimit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := (Page{Limit: tt.limit}).Normalize().Limit; got != tt.want {
				t.Errorf("Normalize() limit = %d, want %d", got, tt.want)
			}
		})
	}
}

// Clamping rather than rejecting the upper bound is deliberate: a caller
// asking for ten thousand rows gets a thousand and a cursor, which is more
// useful than an error telling them to ask again.
func TestNormalizeClampsRatherThanFailing(t *testing.T) {
	t.Parallel()

	got := Page{Limit: 999999}.Normalize()

	if got.Limit != MaxPageLimit {
		t.Errorf("an oversized request was not clamped: %d", got.Limit)
	}
}

func TestNormalizePreservesTheCursor(t *testing.T) {
	t.Parallel()

	cursor := id.MustNew("bus")

	got := Page{Limit: 0, After: cursor}.Normalize()
	if got.After != cursor {
		t.Errorf("Normalize dropped the cursor: %s", got.After)
	}
}

func TestIsFirst(t *testing.T) {
	t.Parallel()

	if !(Page{}).IsFirst() {
		t.Error("a page with no cursor should be the first")
	}

	if (Page{After: id.MustNew("bus")}).IsFirst() {
		t.Error("a page with a cursor is not the first")
	}
}

// The cursor sorts chronologically because the identifier does. That is what
// makes keyset pagination a meaningful order without a separate sort column,
// so it is worth pinning here as well as in pkg/id.
func TestCursorsOrderChronologically(t *testing.T) {
	t.Parallel()

	first := id.MustNew("bus")
	second := id.MustNew("bus")

	if first.String() >= second.String() {
		t.Error("identifiers no longer sort chronologically; keyset " +
			"pagination would return rows in an arbitrary order")
	}
}

// A compile-time check that the interfaces are implementable as written. An
// interface nobody can satisfy is a design error that otherwise surfaces only
// when the first real store tries.
type fakeStore struct{}

func (fakeStore) Migrate(context.Context) error { return nil }
func (fakeStore) Ping(context.Context) error    { return nil }
func (fakeStore) Close() error                  { return nil }
func (fakeStore) StoreName() string             { return "fake" }

func (fakeStore) TemporalEntities() []string { return []string{"bus"} }

func (fakeStore) AsOfSupported() bitemporal.Instant { return bitemporal.Beginning() }

var (
	_ Lifecycle = fakeStore{}
	_ Storable  = fakeStore{}
	_ Temporal  = fakeStore{}
)

func TestStorableComposesLifecycle(t *testing.T) {
	t.Parallel()

	// A Storable must be usable wherever a Lifecycle is wanted, since the host
	// collects stores generically for health checks and migration.
	var s Storable = fakeStore{}

	var lifecycle Lifecycle = s
	if err := lifecycle.Ping(t.Context()); err != nil {
		t.Errorf("Ping: %v", err)
	}
}
