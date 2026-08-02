package provenance_test

import (
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/provenance"
	"github.com/xraph/blackstart/extensions/provenance/store/memory"
)

func TestTheExtensionIdentifiesItself(t *testing.T) {
	e := provenance.New()

	if e.Name() != provenance.ExtensionName {
		t.Errorf("name %q, want %q", e.Name(), provenance.ExtensionName)
	}

	if e.Version() == "" {
		t.Error("the extension reports no version")
	}

	if e.Description() == "" {
		t.Error("the extension has no description; `blackstart extensions` would print a blank")
	}
}

// The recorder holds its own store and publishes an interface. Extensions that
// record provenance depend on that interface rather than on this extension's
// startup order.
func TestTheExtensionDependsOnNothing(t *testing.T) {
	if deps := provenance.New().Dependencies(); len(deps) != 0 {
		t.Errorf("declared dependencies %v", deps)
	}
}

func TestTheEngineIsAbsentBeforeStart(t *testing.T) {
	if e := provenance.New().Engine(); e != nil {
		t.Error("an engine existed before Start")
	}
}

func TestStoppingBeforeStartingIsSafe(t *testing.T) {
	if err := provenance.New().Stop(t.Context()); err != nil {
		t.Errorf("Stop before Start: %v", err)
	}
}

func TestASuppliedStoreIsUsed(t *testing.T) {
	if err := provenance.New(provenance.WithStore(memory.New())).Health(t.Context()); err != nil {
		t.Errorf("the supplied store was not used: %v", err)
	}
}

// The Engine satisfies the narrow Recorder interface other extensions take.
// If it stopped doing so, every consumer would break at its own call site
// rather than here, where the cause is visible.
func TestTheEngineIsARecorder(t *testing.T) {
	var _ provenance.Recorder = (*provenance.Engine)(nil)
}

func TestResolvingTooEarlySaysHowToFixIt(t *testing.T) {
	message := provenance.ErrResolvedTooEarly

	for _, want := range []string{"Dependencies()", provenance.ExtensionName, "not built yet"} {
		if !strings.Contains(message, want) {
			t.Errorf("the resolve-too-early message does not mention %q: %s", want, message)
		}
	}
}
