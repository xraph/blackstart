package fabric

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/fabriq"
	"github.com/xraph/fabriq/core/command"
	"github.com/xraph/fabriq/core/registry"
	"github.com/xraph/forge"
	"github.com/xraph/grove"

	"github.com/xraph/blackstart/internal/entities"
)

type model struct {
	grove.BaseModel `grove:"table:fabric_test_rows"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`
	Name     string `grove:"name"`
}

type contributor struct {
	name  string
	specs []registry.EntitySpec
}

func (c contributor) ExtensionName() string        { return c.name }
func (c contributor) Specs() []registry.EntitySpec { return c.specs }

func registryWithGraph(t *testing.T) *registry.Registry {
	t.Helper()

	reg, err := entities.Build(contributor{
		name: "probe",
		specs: []registry.EntitySpec{{
			Name:      "row",
			Kind:      registry.KindAggregate,
			Model:     (*model)(nil),
			GraphNode: "Row",
		}},
	})
	if err != nil {
		t.Fatalf("building the registry: %v", err)
	}

	return reg
}

func registryWithoutGraph(t *testing.T) *registry.Registry {
	t.Helper()

	reg, err := entities.Build(contributor{
		name: "probe",
		specs: []registry.EntitySpec{{
			Name:  "row",
			Kind:  registry.KindAggregate,
			Model: (*model)(nil),
		}},
	})
	if err != nil {
		t.Fatalf("building the registry: %v", err)
	}

	return reg
}

func newApp(t *testing.T) forge.App {
	t.Helper()

	return forge.NewApp(forge.AppConfig{
		Name:        "fabric-test",
		Version:     "0.0.0",
		Environment: "test",
	})
}

func TestRegisterRejectsAMissingRegistry(t *testing.T) {
	t.Parallel()

	if err := New(nil).Register(newApp(t)); err == nil {
		t.Error("registered with no entity registry")
	}
}

// Resolving the facade before Start is a wiring error — an extension whose
// Dependencies() omits "fabric". It must say that rather than hand back a nil
// that fails later somewhere unrelated.
func TestResolvingBeforeStartExplainsTheWiringError(t *testing.T) {
	t.Parallel()

	ext := New(registryWithoutGraph(t))

	app := newApp(t)
	if err := ext.Register(app); err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, err := forge.Inject[*fabriq.Fabriq](app.Container())
	if err == nil {
		t.Fatal("resolved the facade before Start")
	}

	if !strings.Contains(err.Error(), "Dependencies()") {
		t.Errorf("the error should name the likely cause, got %q", err)
	}
}

// Fabriq's own forgeext returns nil here, leaving ordering to registration
// order. Naming grove lets Forge's topological sort enforce it instead.
func TestDependenciesNamesGrove(t *testing.T) {
	t.Parallel()

	got := New(registryWithoutGraph(t)).Dependencies()

	if len(got) != 1 || got[0] != "grove" {
		t.Errorf("Dependencies() = %v, want [grove]", got)
	}
}

func TestStartRequiresRedisWhenRunningTheWorker(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.RequireGraph = false
	cfg.RunWorker = true
	cfg.Fabriq.Postgres.DSN = "postgres://localhost/nonexistent"

	ext := New(registryWithoutGraph(t), WithConfig(cfg))

	app := newApp(t)
	if err := ext.Register(app); err != nil {
		t.Fatalf("Register: %v", err)
	}

	err := ext.Start(t.Context())
	if err == nil {
		t.Fatal("started the worker with no Redis address")
	}

	if !strings.Contains(err.Error(), "outbox") {
		t.Errorf("the error should explain the consequence, got %q", err)
	}
}

func TestStopBeforeStartIsSafe(t *testing.T) {
	t.Parallel()

	if err := New(registryWithoutGraph(t)).Stop(t.Context()); err != nil {
		t.Errorf("Stop before Start: %v", err)
	}
}

func TestHealthBeforeStartReportsNotStarted(t *testing.T) {
	t.Parallel()

	err := New(registryWithoutGraph(t)).Health(t.Context())
	if err == nil {
		t.Fatal("reported healthy before Start")
	}
}

// The whole reason this package exists rather than using forgeext: lifecycle
// hooks reach fabriq.Open. This asserts they are carried through the config,
// since the alternative — silently dropping them — would leave audit
// non-gapless with no symptom.
type recordingHook struct{ calls int }

func (h *recordingHook) OnChange(context.Context, command.Tx, command.Change) error {
	h.calls++

	return nil
}

func TestLifecycleHooksAreCarried(t *testing.T) {
	t.Parallel()

	hook := &recordingHook{}

	ext := New(registryWithoutGraph(t), WithLifecycleHook(hook))

	if got := len(ext.config.LifecycleHooks); got != 1 {
		t.Fatalf("the extension holds %d lifecycle hooks, want 1", got)
	}

	// And it satisfies the interface Fabriq will call it through, which is
	// what makes carrying it through the config meaningful.
	var _ command.LifecycleHook = hook
}
