package provenance_test

import (
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/fabric"
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

// One dependency, and only one. The default store borrows the fabric
// extension's Fabriq facade, so fabric has to start first; everything else that
// records provenance depends on the Recorder interface rather than on this
// extension's startup order, which is what publishing an interface is for.
func TestTheExtensionDependsOnlyOnFabric(t *testing.T) {
	deps := provenance.New().Dependencies()

	if len(deps) != 1 || deps[0] != fabric.ExtensionName {
		t.Errorf("declared dependencies %v, want only %q", deps, fabric.ExtensionName)
	}
}

// The store is built in Start now rather than in New, so an extension that was
// constructed and never started holds none — the state a failed startup leaves
// behind, and the one Stop runs against.
func TestHealthBeforeStartSaysSo(t *testing.T) {
	err := provenance.New().Health(t.Context())
	if err == nil {
		t.Fatal("an unstarted extension reported itself healthy")
	}

	if !strings.Contains(err.Error(), "has not started") {
		t.Errorf("the message should say why, got %v", err)
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
