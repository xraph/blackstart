package contingency_test

import (
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/contingency"
	"github.com/xraph/blackstart/extensions/contingency/store/memory"
)

func TestTheExtensionIdentifiesItself(t *testing.T) {
	e := contingency.New()

	if e.Name() != contingency.ExtensionName {
		t.Errorf("name %q, want %q", e.Name(), contingency.ExtensionName)
	}

	if e.Version() == "" {
		t.Error("the extension reports no version")
	}

	if e.Description() == "" {
		t.Error("the extension has no description; `blackstart extensions` would print a blank")
	}
}

func TestTheExtensionDependsOnNothing(t *testing.T) {
	if deps := contingency.New().Dependencies(); len(deps) != 0 {
		t.Errorf("declared dependencies %v", deps)
	}
}

func TestTheEngineIsAbsentBeforeStart(t *testing.T) {
	if e := contingency.New().Engine(); e != nil {
		t.Error("an engine existed before Start")
	}
}

// Forge stops everything it registered, including extensions whose Start
// failed, so stopping before starting must not panic.
func TestStoppingBeforeStartingIsSafe(t *testing.T) {
	if err := contingency.New().Stop(t.Context()); err != nil {
		t.Errorf("Stop before Start: %v", err)
	}
}

// Health reports on the store, not the solver. Going unhealthy because a
// solver was restarting would take the application down for something the
// client recovers from on the very next call.
func TestHealthReportsOnTheStoreOnly(t *testing.T) {
	e := contingency.New(contingency.WithStore(memory.New()))

	if err := e.Health(t.Context()); err != nil {
		t.Errorf("Health with a reachable store and no solver: %v", err)
	}
}

// The default has to be conspicuous in a stored record rather than read like a
// real version. A run labeled "unconfigured" prompts the question; one labeled
// "0.0.0" does not.
func TestTheDefaultSolverVersionIsObviouslyAPlaceholder(t *testing.T) {
	if contingency.DefaultSolverVersion == "" {
		t.Fatal("the default solver version is empty; NewEngine would reject it")
	}

	if strings.ContainsAny(contingency.DefaultSolverVersion, "0123456789") {
		t.Errorf("%q reads like a real version", contingency.DefaultSolverVersion)
	}
}

// The message a caller sees when it resolves the engine too early has to name
// the fix. Asserted against the exported constant rather than a copy, because
// a test holding its own copy would keep passing through any rewording.
func TestResolvingTooEarlySaysHowToFixIt(t *testing.T) {
	message := contingency.ErrResolvedTooEarly

	for _, want := range []string{"Dependencies()", contingency.ExtensionName, "not built yet"} {
		if !strings.Contains(message, want) {
			t.Errorf("the resolve-too-early message does not mention %q: %s", want, message)
		}
	}
}
