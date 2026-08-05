package scenario_test

// The queue drainer's lifecycle, from outside the package. The loop itself is
// exercised in runner_internal_test.go, which can reach the engine a started
// extension would have built.

import (
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/scenario"
)

// Shutdown is called on every registered extension whether or not Run ever
// started a loop — a host that fails partway through startup shuts down
// everything it has.
func TestShuttingDownBeforeRunningIsSafe(t *testing.T) {
	if err := scenario.New().Shutdown(t.Context()); err != nil {
		t.Errorf("Shutdown before Run: %v", err)
	}
}

// A process that does not drain the queue starts no loop. bsctl is that
// process: a short-lived command claiming a run would strand it running,
// unfinished, and invisible to every other worker.
func TestAProcessThatDrainsNothingStartsNoLoop(t *testing.T) {
	if err := scenario.New().Run(t.Context()); err != nil {
		t.Errorf("Run without the worker flag: %v", err)
	}
}

// A worker before Start has no engine to tick, and saying so is better than
// dereferencing a nil on the first tick, in a goroutine, where the panic
// names this file rather than the wiring mistake.
func TestRunningTheWorkerBeforeStartIsRefused(t *testing.T) {
	err := scenario.New(scenario.WithWorker(true)).Run(t.Context())
	if err == nil {
		t.Fatal("the worker started before the engine existed")
	}

	if !strings.Contains(err.Error(), "Start") {
		t.Errorf("the refusal does not say what is missing: %v", err)
	}
}
