package powerflow_test

import (
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/powerflow"
	"github.com/xraph/blackstart/extensions/powerflow/store/memory"
)

func TestTheExtensionIdentifiesItself(t *testing.T) {
	e := powerflow.New()

	if e.Name() != powerflow.ExtensionName {
		t.Errorf("name %q, want %q", e.Name(), powerflow.ExtensionName)
	}

	if e.Version() == "" {
		t.Error("the extension reports no version")
	}

	if e.Description() == "" {
		t.Error("the extension has no description; `blackstart extensions` would print a blank")
	}
}

// Nothing to depend on: this extension reaches the solver over gRPC and holds
// its results in its own store. Naming a dependency it does not have would
// constrain startup order for no reason.
// The fabric extension, because the default store borrows its Fabriq facade.
// Results are durable rather than in-memory: a number that vanishes when the
// process exits cannot be cited.
func TestTheExtensionDependsOnTheFabric(t *testing.T) {
	deps := powerflow.New().Dependencies()

	if len(deps) != 1 || deps[0] != "fabric" {
		t.Errorf("declared dependencies %v", deps)
	}
}

// Before Start there is no engine, because it needs a solver connection that
// is not open at registration time. The accessor returns nil rather than a
// half-built engine, so a caller cannot mistake one for the other.
func TestTheEngineIsAbsentBeforeStart(t *testing.T) {
	if e := powerflow.New().Engine(); e != nil {
		t.Error("an engine existed before Start")
	}
}

// Stopping an extension that never started must not panic. Forge stops
// everything it registered, including extensions whose Start failed.
func TestStoppingBeforeStartingIsSafe(t *testing.T) {
	if err := powerflow.New().Stop(t.Context()); err != nil {
		t.Errorf("Stop before Start: %v", err)
	}
}

// Health reports on the store, not the solver. A power-flow extension that
// went unhealthy because a solver was restarting would take the application's
// health down for something the client recovers from on the very next call.
func TestHealthReportsOnTheStoreOnly(t *testing.T) {
	e := powerflow.New(powerflow.WithStore(memory.New()))

	if err := e.Health(t.Context()); err != nil {
		t.Errorf("Health with a reachable store and no solver: %v", err)
	}
}

func TestAStoreCanBeSupplied(t *testing.T) {
	supplied := memory.New()

	e := powerflow.New(powerflow.WithStore(supplied))

	// Observable through Health, which is the only thing the extension does
	// with the store before Start.
	if err := e.Health(t.Context()); err != nil {
		t.Errorf("the supplied store was not used: %v", err)
	}
}

// The message a caller sees when it resolves the engine before this extension
// has started has to name the fix. "engine is nil" sends the reader to the
// source; naming Dependencies() tells them what to write.
//
// Asserted against the exported message rather than a copy of it, because a
// test holding its own copy would keep passing through any rewording — which
// is the same as not testing it. Exercising the provider itself would need a
// started Forge application, and Register is not called until then.
func TestResolvingTooEarlySaysHowToFixIt(t *testing.T) {
	message := powerflow.ErrResolvedTooEarly

	for _, want := range []string{"Dependencies()", powerflow.ExtensionName, "not built yet"} {
		if !strings.Contains(message, want) {
			t.Errorf("the resolve-too-early message does not mention %q: %s", want, message)
		}
	}
}
