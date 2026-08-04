package cascade_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xraph/forge"

	"github.com/xraph/blackstart/extensions/cascade"
	"github.com/xraph/blackstart/extensions/cascade/store/memory"
)

func TestTheExtensionIdentifiesItself(t *testing.T) {
	e := cascade.New()

	if e.Name() != cascade.ExtensionName {
		t.Errorf("name %q, want %q", e.Name(), cascade.ExtensionName)
	}

	if e.Version() == "" {
		t.Error("the extension reports no version")
	}

	if e.Description() == "" {
		t.Error("the extension has no description; `blackstart extensions` would print a blank")
	}
}

// The fabric extension is named so the dependency ordering is already
// correct for a future Fabriq-backed store, the same reason contingency
// names it despite reaching its own solver over gRPC directly.
func TestTheExtensionDependsOnTheFabric(t *testing.T) {
	deps := cascade.New().Dependencies()

	if len(deps) != 1 || deps[0] != "fabric" {
		t.Errorf("declared dependencies %v", deps)
	}
}

func TestTheEngineIsAbsentBeforeStart(t *testing.T) {
	if e := cascade.New().Engine(); e != nil {
		t.Error("an engine existed before Start")
	}
}

// Forge stops everything it registered, including extensions whose Start
// failed, so stopping before starting must not panic.
func TestStoppingBeforeStartingIsSafe(t *testing.T) {
	if err := cascade.New().Stop(t.Context()); err != nil {
		t.Errorf("Stop before Start: %v", err)
	}
}

// Health reports on the store, not the solver. Going unhealthy because a
// solver was restarting would take the application down for something the
// client recovers from on the very next call.
func TestHealthReportsOnTheStoreOnly(t *testing.T) {
	e := cascade.New(cascade.WithStore(memory.New()))

	if err := e.Health(t.Context()); err != nil {
		t.Errorf("Health with a reachable store and no solver: %v", err)
	}
}

// The message a caller sees when it resolves the engine too early has to name
// the fix. Asserted against the exported constant rather than a copy, because
// a test holding its own copy would keep passing through any rewording.
func TestResolvingTooEarlySaysHowToFixIt(t *testing.T) {
	message := cascade.ErrResolvedTooEarly

	for _, want := range []string{"Dependencies()", cascade.ExtensionName, "not built yet"} {
		if !strings.Contains(message, want) {
			t.Errorf("the resolve-too-early message does not mention %q: %s", want, message)
		}
	}
}

// This is the important test in this file. Forge's router type-asserts
// forge.ControllerWithMiddleware, whose method is Middleware() — singular. A
// controller that declared Middlewares() instead would satisfy nothing: the
// assertion would fail silently at runtime and every cascade route would
// register unguarded. api.go carries a `var _ forge.ControllerWithMiddleware`
// assertion that turns that mistake into a compile error; this test is the
// backstop if that line were ever deleted — it would still fail here, at
// runtime, rather than shipping silently.
//
// Renaming Controller.Middleware to Controller.Middlewares makes both this
// test and the compile-time assertion in api.go fail: the assertion first,
// as a build error, and — if that assertion line were also removed — this
// type check second, as a normal test failure reporting `ok == false`.
func TestTheControllerSatisfiesTheMiddlewareInterface(t *testing.T) {
	var controller any = cascade.NewController(nil, nil)

	if _, ok := controller.(forge.ControllerWithMiddleware); !ok {
		t.Fatal("Controller does not satisfy forge.ControllerWithMiddleware; " +
			"Forge's router would register every cascade route unguarded")
	}
}

// Listing every run across every network is not a question anyone asks and
// would page forever, so the endpoint refuses to answer it rather than
// silently returning everything. Exercised through a real router, not a
// direct call to the handler, because the thing under test is what an actual
// HTTP client sees: a 400 with an explanation, not a Go error value.
func TestListRunsRejectsAnEmptyModelHash(t *testing.T) {
	app := forge.New(forge.WithAppName("cascade-test"))

	if err := app.RegisterController(cascade.NewController(nil, nil)); err != nil {
		t.Fatalf("RegisterController: %v", err)
	}

	if err := app.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = app.Stop(t.Context()) }()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cascade/runs", nil)
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	if !strings.Contains(rec.Body.String(), "modelHash") {
		t.Errorf("the rejection does not explain itself: %s", rec.Body.String())
	}
}
