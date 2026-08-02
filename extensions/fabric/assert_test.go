package fabric

import (
	"strings"
	"testing"
)

// The check Fabriq does not make. A GraphNode with no graph engine configured
// is skipped silently, so the system boots and maintains an empty graph view
// with no error anywhere.
func TestRegisterRefusesGraphEntitiesWithNoGraphEngine(t *testing.T) {
	t.Parallel()

	ext := New(registryWithGraph(t))

	err := ext.Register(newApp(t))
	if err == nil {
		t.Fatal("started with graph entities declared and no graph engine; " +
			"fabriq would skip them silently and the graph view would be empty")
	}

	// The message has to name the entities, or the operator has to go hunting
	// for which spec caused it.
	for _, want := range []string{"row", "falkordb.addr", "silently"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should mention %q, got %q", want, err)
		}
	}
}

func TestRegisterAcceptsGraphEntitiesWhenTheEngineIsConfigured(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Fabriq.FalkorDB.Addr = "localhost:6379"

	ext := New(registryWithGraph(t), WithConfig(cfg))

	if err := ext.Register(newApp(t)); err != nil {
		t.Errorf("Register: %v", err)
	}
}

func TestRegisterAcceptsARegistryWithNoGraphEntities(t *testing.T) {
	t.Parallel()

	ext := New(registryWithoutGraph(t))

	if err := ext.Register(newApp(t)); err != nil {
		t.Errorf("Register: %v", err)
	}
}

// The assertion is a guard, not a policy: a deployment that genuinely does not
// want the graph can say so, and then it is a decision rather than an
// accident.
func TestRequireGraphCanBeDisabled(t *testing.T) {
	t.Parallel()

	ext := New(registryWithGraph(t), WithRequireGraph(false))

	if err := ext.Register(newApp(t)); err != nil {
		t.Errorf("Register with the assertion disabled: %v", err)
	}
}
