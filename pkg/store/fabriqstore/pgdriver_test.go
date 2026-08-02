package fabriqstore

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/grove"
	"github.com/xraph/grove/drivers/pgdriver"
)

// stubDriver satisfies grove.GroveDriver without being PostgreSQL. It exists
// to exercise the case pgdriver.Unwrap would panic on.
type stubDriver struct{ name string }

func (s stubDriver) Name() string               { return s.name }
func (s stubDriver) Close() error               { return nil }
func (s stubDriver) Ping(context.Context) error { return nil }

// The whole point of the function, and the reason Unwrap is banned. Grove's
// Unwrap panics here; this returns an error naming the driver it found.
func TestPgOrErrRejectsAnotherDriverWithoutPanicking(t *testing.T) {
	t.Parallel()

	gdb, err := grove.Open(stubDriver{name: "sqlite"})
	if err != nil {
		t.Fatalf("grove.Open: %v", err)
	}

	got, err := PgOrErr(gdb)
	if err == nil {
		t.Fatalf("accepted a %q driver and returned %#v", "sqlite", got)
	}

	if !strings.Contains(err.Error(), "sqlite") {
		t.Errorf("the error should name the driver actually found, got %q", err)
	}

	// The message should say why Postgres specifically, since "wrong driver"
	// alone leaves the reader wondering whether it is a preference.
	for _, want := range []string{"PostGIS", "btree_gist"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should explain why PostgreSQL is required "+
				"(missing %q): %q", want, err)
		}
	}
}

func TestPgOrErrHandlesAbsentHandles(t *testing.T) {
	t.Parallel()

	if _, err := PgOrErr(nil); err == nil {
		t.Error("accepted a nil handle")
	}
}

// A driverless handle is not a case grove is expected to produce, but the
// alternative to checking is a nil dereference inside a type assertion, which
// reads as a crash in someone else's code.
func TestPgOrErrHandlesANilDriver(t *testing.T) {
	t.Parallel()

	var gdb grove.DB

	if _, err := PgOrErr(&gdb); err == nil {
		t.Error("accepted a handle with no driver")
	}
}

// The premise of this whole file, made checkable rather than asserted.
//
// Grove's own README recommends pgdriver.Unwrap. On any non-PostgreSQL handle
// it panics, which is why .golangci.yml forbids it everywhere except here,
// where the test exclusion applies. If a future grove release makes Unwrap
// return an error instead, this test fails and the ban can be reconsidered.
func TestGroveUnwrapPanicsWherePgOrErrReturns(t *testing.T) {
	t.Parallel()

	gdb, err := grove.Open(stubDriver{name: "sqlite"})
	if err != nil {
		t.Fatalf("grove.Open: %v", err)
	}

	if _, err := PgOrErr(gdb); err == nil {
		t.Fatal("precondition: PgOrErr should have returned an error")
	}

	defer func() {
		if recover() == nil {
			t.Error("pgdriver.Unwrap no longer panics on a non-Postgres " +
				"driver; the forbidigo ban in .golangci.yml can be revisited")
		}
	}()

	_ = pgdriver.Unwrap(gdb)
}
