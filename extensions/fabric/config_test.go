package fabric

import "testing"

func TestOptionsCompose(t *testing.T) {
	t.Parallel()

	ext := New(
		registryWithoutGraph(t),
		WithTenant("other"),
		WithWorker(false),
		WithGroveDatabase("secondary"),
		WithRequireGraph(false),
	)

	if ext.Tenant() != "other" {
		t.Errorf("Tenant() = %q", ext.Tenant())
	}

	if ext.config.RunWorker {
		t.Error("WithWorker(false) did not take effect")
	}

	if ext.config.GroveDatabase != "secondary" {
		t.Errorf("GroveDatabase = %q", ext.config.GroveDatabase)
	}
}

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()

	if cfg.Tenant != DefaultTenant {
		t.Errorf("default tenant = %q", cfg.Tenant)
	}

	if !cfg.RunWorker {
		t.Error("the default should run the worker; without one anywhere the " +
			"outbox never drains")
	}

	if !cfg.RequireGraph {
		t.Error("the default should assert graph configuration")
	}
}
