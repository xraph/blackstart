package scenario

// In-package, like the rest of this extension's tests: the HTTP surface is
// exercised over an engine built from the same fakes the pipeline tests use,
// and the limit assertions need a store wrapper that sees what the controller
// asked for.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xraph/forge"

	scn "github.com/xraph/blackstart/extensions/scenario/scenario"
	scnstore "github.com/xraph/blackstart/extensions/scenario/store"
	"github.com/xraph/blackstart/extensions/scenario/store/memory"
	"github.com/xraph/blackstart/pkg/id"
)

// --- A store that remembers what limit it was given ---

// recordingStore delegates everything and records the limits the two list
// methods received.
//
// A wrapper rather than a counter inside the memory store, because the thing
// under test is what the CONTROLLER passes down. Both stores document that a
// limit of zero or less returns everything, so a controller that forwarded a
// caller's missing limit unchanged would be asking for the whole table and no
// assertion inside the store could tell that apart from a caller that meant
// it.
type recordingStore struct {
	scnstore.Store

	scenarioLimits []int
	runLimits      []int
}

func recording(inner scnstore.Store) *recordingStore {
	return &recordingStore{Store: inner}
}

func (s *recordingStore) Scenarios() scn.Store { return &recordingScenarios{parent: s} }

func (s *recordingStore) Runs() scn.RunStore { return &recordingRuns{parent: s} }

type recordingScenarios struct{ parent *recordingStore }

func (s *recordingScenarios) Create(
	ctx context.Context, in scn.Scenario,
) (scn.Scenario, error) {
	return s.parent.Store.Scenarios().Create(ctx, in)
}

func (s *recordingScenarios) Get(ctx context.Context, want id.ID) (scn.Scenario, error) {
	return s.parent.Store.Scenarios().Get(ctx, want)
}

func (s *recordingScenarios) Update(
	ctx context.Context, in scn.Scenario,
) (scn.Scenario, error) {
	return s.parent.Store.Scenarios().Update(ctx, in)
}

func (s *recordingScenarios) List(ctx context.Context, limit int) ([]scn.Scenario, error) {
	s.parent.scenarioLimits = append(s.parent.scenarioLimits, limit)

	return s.parent.Store.Scenarios().List(ctx, limit)
}

type recordingRuns struct{ parent *recordingStore }

func (s *recordingRuns) Create(
	ctx context.Context, in scn.ScenarioRun,
) (scn.ScenarioRun, error) {
	return s.parent.Store.Runs().Create(ctx, in)
}

func (s *recordingRuns) Get(ctx context.Context, want id.ID) (scn.ScenarioRun, error) {
	return s.parent.Store.Runs().Get(ctx, want)
}

func (s *recordingRuns) ListByScenario(
	ctx context.Context, scenarioID id.ID, limit int,
) ([]scn.ScenarioRun, error) {
	s.parent.runLimits = append(s.parent.runLimits, limit)

	return s.parent.Store.Runs().ListByScenario(ctx, scenarioID, limit)
}

func (s *recordingRuns) Claim(ctx context.Context) (scn.ScenarioRun, error) {
	return s.parent.Store.Runs().Claim(ctx)
}

func (s *recordingRuns) Finish(
	ctx context.Context, in scn.ScenarioRun,
) (scn.ScenarioRun, error) {
	return s.parent.Store.Runs().Finish(ctx, in)
}

// --- Harness ---

// surface brings up a real Forge router over the controller, so what these
// tests read is what an HTTP client would receive rather than a Go value a
// handler returned.
type surface struct {
	app    forge.App
	engine *Engine
	store  *recordingStore
}

func newSurface(t *testing.T) *surface {
	t.Helper()

	store := recording(memory.New())

	engine, err := NewEngine(store, newFakes().collaborators(), nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	app := forge.New(forge.WithAppName("scenario-test"))

	if err := app.RegisterController(NewController(engine, nil)); err != nil {
		t.Fatalf("RegisterController: %v", err)
	}

	if err := app.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	t.Cleanup(func() { _ = app.Stop(context.WithoutCancel(t.Context())) })

	return &surface{app: app, engine: engine, store: store}
}

func (s *surface) do(method, path, body string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}

	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	s.app.Router().ServeHTTP(rec, req)

	return rec
}

// --- The guard ---

// Forge's router type-asserts forge.ControllerWithMiddleware, whose method is
// Middleware() — singular. A controller that declared Middlewares() instead
// would satisfy nothing: the assertion would fail silently at runtime and
// every scenario route would register unguarded, including the two that
// write. api.go carries a compile-time assertion; this is the backstop if
// that line were ever deleted.
func TestTheControllerSatisfiesTheMiddlewareInterface(t *testing.T) {
	var controller any = NewController(nil, nil)

	if _, ok := controller.(forge.ControllerWithMiddleware); !ok {
		t.Fatal("Controller does not satisfy forge.ControllerWithMiddleware; " +
			"Forge's router would register every scenario route unguarded")
	}
}

// --- Defining ---

// A manifest that cannot be reproduced is refused, and the refusal names the
// part that is missing. "Invalid manifest" would leave a caller to guess
// among five fields.
func TestDefiningAnUnreproducibleManifestNamesWhatIsMissing(t *testing.T) {
	s := newSurface(t)

	rec := s.do(http.MethodPost, "/api/v1/scenario/scenarios",
		`{"name":"storm","manifest":{"weather_cycle":"wxc_1",`+
			`"valid_at":"2026-08-03T18:00:00Z","policy":"all"}}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400; body: %s", rec.Code, rec.Body.String())
	}

	if !strings.Contains(rec.Body.String(), "model hash") {
		t.Errorf("the rejection does not name the missing field: %s", rec.Body.String())
	}
}

// An explicit policy with no branches is the other way a manifest lies about
// itself: empty would silently become "screen everything".
func TestDefiningAnExplicitPolicyWithNoBranchesIsRefused(t *testing.T) {
	s := newSurface(t)

	rec := s.do(http.MethodPost, "/api/v1/scenario/scenarios",
		`{"name":"storm","manifest":{"model_hash":"sha256:abc","weather_cycle":"wxc_1",`+
			`"valid_at":"2026-08-03T18:00:00Z","policy":"explicit"}}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400; body: %s", rec.Code, rec.Body.String())
	}

	if !strings.Contains(rec.Body.String(), "branch list") {
		t.Errorf("the rejection does not name the missing field: %s", rec.Body.String())
	}
}

// An identifier of another type is refused rather than treated as a scenario
// nobody has defined — which is how a mistyped id becomes a second scenario.
func TestDefiningAgainstAnIdentifierOfAnotherTypeIsRefused(t *testing.T) {
	s := newSurface(t)

	body, err := json.Marshal(defineScenarioRequest{
		ID:       "casc_01k1ykqqzmf3rt7pqvxb1z0hxa",
		Name:     "storm",
		Manifest: validManifest(),
	})
	if err != nil {
		t.Fatalf("marshaling the request: %v", err)
	}

	rec := s.do(http.MethodPost, "/api/v1/scenario/scenarios", string(body))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
}

// A definition comes back with the identifier the platform minted, because
// that identifier is the only way the caller can queue a run of it.
func TestDefiningAnswersWithTheStoredScenario(t *testing.T) {
	s := newSurface(t)

	body, err := json.Marshal(defineScenarioRequest{Name: "storm", Manifest: validManifest()})
	if err != nil {
		t.Fatalf("marshaling the request: %v", err)
	}

	rec := s.do(http.MethodPost, "/api/v1/scenario/scenarios", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var got scn.Scenario
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding the response: %v; body: %s", err, rec.Body.String())
	}

	if got.ID.IsZero() {
		t.Error("the stored scenario came back with no identifier")
	}

	if got.Manifest.ModelHash != validManifest().ModelHash {
		t.Errorf("model hash %q, want %q", got.Manifest.ModelHash, validManifest().ModelHash)
	}
}
