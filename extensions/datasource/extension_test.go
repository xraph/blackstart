package datasource_test

import (
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/datasource"
	"github.com/xraph/blackstart/extensions/datasource/store/memory"
	"github.com/xraph/blackstart/extensions/fabric"
)

func TestTheExtensionIdentifiesItself(t *testing.T) {
	e := datasource.New()

	if e.Name() != datasource.ExtensionName {
		t.Errorf("name %q, want %q", e.Name(), datasource.ExtensionName)
	}

	if e.Version() == "" {
		t.Error("the extension reports no version")
	}

	if e.Description() == "" {
		t.Error("the extension has no description; `blackstart extensions` would print a blank")
	}
}

// Named rather than left to registration order. Fabriq's own forgeext returns
// nil here, which is a silent trap the first time two extensions are
// registered the other way round.
func TestTheExtensionDependsOnTheFabric(t *testing.T) {
	deps := datasource.New().Dependencies()

	if len(deps) != 1 || deps[0] != fabric.ExtensionName {
		t.Errorf("dependencies %v, want just %q", deps, fabric.ExtensionName)
	}
}

func TestTheEngineIsAbsentBeforeStart(t *testing.T) {
	if e := datasource.New().Engine(); e != nil {
		t.Error("an engine existed before Start")
	}
}

// Forge stops everything it registered, including extensions whose Start
// failed, so stopping before starting must not panic — and with no store there
// is nothing to close.
func TestStoppingBeforeStartingIsSafe(t *testing.T) {
	if err := datasource.New().Stop(t.Context()); err != nil {
		t.Errorf("Stop before Start: %v", err)
	}
}

// Health before Start reports the reason rather than succeeding vacuously. An
// extension with no store is not healthy; it is unstarted, and saying so is
// what distinguishes a wiring problem from a database outage.
func TestHealthBeforeStartSaysWhy(t *testing.T) {
	err := datasource.New().Health(t.Context())
	if err == nil {
		t.Fatal("an unstarted extension reported itself healthy")
	}

	if !strings.Contains(err.Error(), "not started") {
		t.Errorf("message %q does not say the extension is unstarted", err)
	}
}

func TestASuppliedStoreIsUsed(t *testing.T) {
	e := datasource.New(datasource.WithStore(memory.New()))

	if err := e.Health(t.Context()); err != nil {
		t.Errorf("the supplied store was not used: %v", err)
	}
}

// The message a caller sees when it resolves the engine before this extension
// has started has to name the fix. Asserted against the exported constant
// rather than a copy, because a test holding its own copy would keep passing
// through any rewording.
func TestResolvingTooEarlySaysHowToFixIt(t *testing.T) {
	message := datasource.ErrResolvedTooEarly

	for _, want := range []string{"Dependencies()", datasource.ExtensionName, "not built yet"} {
		if !strings.Contains(message, want) {
			t.Errorf("the resolve-too-early message does not mention %q: %s", want, message)
		}
	}
}
