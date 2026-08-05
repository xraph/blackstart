package scenario_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/scenario"
	"github.com/xraph/blackstart/extensions/scenario/store/memory"
)

func TestTheExtensionIdentifiesItself(t *testing.T) {
	e := scenario.New()

	if e.Name() != scenario.ExtensionName {
		t.Errorf("name %q, want %q", e.Name(), scenario.ExtensionName)
	}

	if e.Version() == "" {
		t.Error("the extension reports no version")
	}

	if e.Description() == "" {
		t.Error("the extension has no description; `blackstart extensions` would print a blank")
	}
}

// Every engine Start resolves out of the container has to be named, or this
// extension's Start is ordered by registration order — which is not something
// this extension can see or control. An engine resolved before its own
// extension started answers with a resolved-too-early error, so a missing
// name here is a startup failure in a deployment nobody changed.
func TestTheExtensionDeclaresWhatItResolves(t *testing.T) {
	deps := scenario.New().Dependencies()

	for _, want := range []string{
		"fabric", "networkmodel", "weather", "geo", "hazard", "contingency", "cascade",
	} {
		if !slices.Contains(deps, want) {
			t.Errorf("declared dependencies %v do not name %q", deps, want)
		}
	}
}

func TestTheEngineIsAbsentBeforeStart(t *testing.T) {
	if e := scenario.New().Engine(); e != nil {
		t.Error("an engine existed before Start")
	}
}

// Forge stops everything it registered, including extensions whose Start
// failed, so stopping before starting must not panic.
func TestStoppingBeforeStartingIsSafe(t *testing.T) {
	if err := scenario.New().Stop(t.Context()); err != nil {
		t.Errorf("Stop before Start: %v", err)
	}
}

// Health before Start reports rather than panicking on a nil store, because
// Forge's health manager polls every registered extension including one whose
// Start failed.
func TestHealthBeforeStartReportsRatherThanPanicking(t *testing.T) {
	if err := scenario.New().Health(t.Context()); err == nil {
		t.Error("an unstarted extension reported healthy")
	}
}

func TestHealthReportsOnTheStore(t *testing.T) {
	e := scenario.New(scenario.WithStore(memory.New()))

	if err := e.Health(t.Context()); err != nil {
		t.Errorf("Health with a reachable store: %v", err)
	}
}

// The message a caller sees when it resolves the engine too early has to name
// the fix. Asserted against the exported constant rather than a copy, because
// a test holding its own copy would keep passing through any rewording.
func TestResolvingTooEarlySaysHowToFixIt(t *testing.T) {
	message := scenario.ErrResolvedTooEarly

	for _, want := range []string{"Dependencies()", scenario.ExtensionName, "not built yet"} {
		if !strings.Contains(message, want) {
			t.Errorf("the resolve-too-early message does not mention %q: %s", want, message)
		}
	}
}
