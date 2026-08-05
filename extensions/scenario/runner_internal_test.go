package scenario

// In-package, so the loop can be started against an engine without standing
// up the seven extensions Start resolves one from. What is under test is the
// loop — that it ticks at all, that it keeps ticking, and that canceling it
// stops it — not the wiring that hands it an engine.

import (
	"context"
	"testing"
	"time"

	scn "github.com/xraph/blackstart/extensions/scenario/scenario"
)

// hosted is an extension holding an engine the way a started one would.
func hosted(t *testing.T, engine *Engine) *Extension {
	t.Helper()

	e := New(WithWorker(true))
	e.engine = engine

	return e
}

// The loop drains what is queued.
//
// The assertion that matters is the transition, not the outcome: a run left
// queued forever is the failure this loop exists to prevent, and it is
// invisible from outside — every write succeeds, the API answers, and the
// scenario simply never produces an answer. A first tick that never happened,
// a ticker that never fired again, or a Tick whose error was swallowed
// without the run moving would all leave it queued.
func TestTheLoopDrainsAQueuedRun(t *testing.T) {
	engine, store := engineWithFakes(t)

	defined, err := engine.Define(t.Context(), scn.Scenario{
		Name: "storm", Manifest: validManifest(),
	})
	if err != nil {
		t.Fatalf("Define: %v", err)
	}

	queued, err := engine.Enqueue(t.Context(), defined.ID)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	ext := hosted(t, engine)

	if err := ext.Run(t.Context()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Shutdown is what makes this test not leak a goroutine into the rest of
	// the package, and it is the same call Forge makes on the way down. Given
	// a live context: t.Context() is already canceled by the time cleanup
	// runs, and Shutdown would report a timeout it did not have.
	t.Cleanup(func() {
		if err := ext.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	})

	deadline := time.Now().Add(2 * time.Second)

	for {
		found, err := store.Runs().Get(t.Context(), queued.ID)
		if err != nil {
			t.Fatalf("reading the run: %v", err)
		}

		if found.Status != scn.StatusQueued && found.Status != scn.StatusRunning {
			// Complete or failed — either is the loop having done its job.
			// Which one it is belongs to the pipeline's own tests, and
			// asserting it here would make this a test of the fakes.
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("the run is still %s; the loop never drained it", found.Status)
		}

		time.Sleep(5 * time.Millisecond)
	}
}

// Shutdown stops the loop and waits for it, rather than returning while a
// tick is still in flight — a worker still claiming runs after the
// application declared itself stopped is how a run ends up half-executed
// against a store that is closing underneath it.
func TestShutdownWaitsForTheLoop(t *testing.T) {
	engine, _ := engineWithFakes(t)

	ext := hosted(t, engine)

	if err := ext.Run(t.Context()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if err := ext.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// The loop is gone, so a second Shutdown has nothing to wait for and must
	// still answer — Forge stops an extension whether or not it was running.
	if err := ext.Shutdown(t.Context()); err != nil {
		t.Errorf("second Shutdown: %v", err)
	}
}
