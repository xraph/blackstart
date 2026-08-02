package testsupport

import (
	"testing"

	"github.com/xraph/vessel"
)

// Container returns a real dependency-injection container.
//
// Real, not a double — and that is not a preference, it is forced. Vessel's
// generic API type-asserts the concrete implementation:
//
//	// vessel/provide.go:609
//	return zero, fmt.Errorf("Inject requires *containerImpl, got %T", c)
//
// The same assertion appears in Provide, ProvideValue and InjectNamed. So
// although vessel.Vessel is an interface alias, substituting an
// implementation of it breaks every typed registration and resolution in the
// codebase. There is no seam here to fake.
//
// The practical consequence for test design: isolate at the store interface,
// not at the container. A service under test takes a store.Store it can be
// handed a memory implementation of; nothing benefits from a fake container,
// and attempting one costs an afternoon before the error above explains why.
func Container(t *testing.T) vessel.Vessel {
	t.Helper()

	c := vessel.New()

	t.Cleanup(func() {
		if err := c.Stop(t.Context()); err != nil {
			t.Logf("stopping the test container: %v", err)
		}
	})

	return c
}

// ProvideValue registers a value in the container, failing the test if it
// cannot be registered.
//
// A registration failure in a test is almost always a mistake in the test
// itself — a duplicate name, or a type the container cannot resolve — and
// surfacing it at the call site beats discovering it as a resolution failure
// three lines later.
func ProvideValue[T any](t *testing.T, c vessel.Vessel, value T) {
	t.Helper()

	if err := vessel.ProvideValue(c, value); err != nil {
		t.Fatalf("registering %T: %v", value, err)
	}
}

// Inject resolves a value from the container, failing the test if it cannot.
func Inject[T any](t *testing.T, c vessel.Vessel) T {
	t.Helper()

	got, err := vessel.Inject[T](c)
	if err != nil {
		var zero T

		t.Fatalf("resolving %T: %v", zero, err)
	}

	return got
}
