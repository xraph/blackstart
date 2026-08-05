package scenario_test

// What the generated OpenAPI document says about this extension's types.
//
// Separate from the runtime tests because the subject is different: these
// assert the CONTRACT a client is generated from, and the two ways it has
// gone wrong here are a component name shared with another extension and a
// field promised as always present that the encoder omits.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/xraph/forge"

	"github.com/xraph/blackstart/extensions/contingency"
	"github.com/xraph/blackstart/extensions/scenario"
)

// spec brings up both run-serving controllers on one router and returns the
// OpenAPI document Forge generates for them.
func spec(t *testing.T) map[string]any {
	t.Helper()

	app := forge.New(
		forge.WithAppName("scenario-spec-test"),
		forge.WithAppRouterOptions(forge.WithOpenAPI(forge.OpenAPIConfig{
			Title:       "scenario spec test",
			Version:     "0",
			SpecPath:    "/openapi.json",
			SpecEnabled: true,
		})),
	)

	// Both, and in this order: contingency is the shipped surface, so it is
	// the one whose component name a collision would move.
	if err := app.RegisterController(contingency.NewController(nil, nil)); err != nil {
		t.Fatalf("registering the contingency controller: %v", err)
	}

	if err := app.RegisterController(scenario.NewController(nil, nil)); err != nil {
		t.Fatalf("registering the scenario controller: %v", err)
	}

	if err := app.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	t.Cleanup(func() { _ = app.Stop(t.Context()) })

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("fetching the specification: status %d", rec.Code)
	}

	var document map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &document); err != nil {
		t.Fatalf("decoding the specification: %v", err)
	}

	return document
}

// dig walks a decoded JSON document by key, failing rather than returning a
// nil that a later assertion would have to interpret.
func dig(t *testing.T, document map[string]any, path ...string) map[string]any {
	t.Helper()

	current := document

	for index, key := range path {
		next, ok := current[key].(map[string]any)
		if !ok {
			t.Fatalf("the specification has no %s", strings.Join(path[:index+1], " → "))
		}

		current = next
	}

	return current
}

// runSchema is the component one endpoint's 200 response points at.
func runSchema(t *testing.T, document map[string]any, path, status string) string {
	t.Helper()

	schema := dig(t, document, "paths", path, "get", "responses", status,
		"content", "application/json", "schema")

	ref, ok := schema["$ref"].(string)
	if !ok {
		t.Fatalf("%s answers with an inline schema rather than a component", path)
	}

	return strings.TrimPrefix(ref, "#/components/schemas/")
}

// This is the important test in this file.
//
// Forge keys an OpenAPI component by bare Go type name, not by the label
// passed to WithResponseSchema. Two extensions serving a type they each
// called Run put both under one component, and the loser's endpoints then
// document the winner's shape — which has happened twice in this repository
// and hid three distinct bugs the second time, including a console panel
// rendering "solver undefined" in production.
//
// Reverting scn.ScenarioRun to a bare Run makes this fail: the two endpoints
// would either share one component name, or Forge would namespace one of them
// by registration order — and the shipped surface, contingency's, could be
// the one that moves.
func TestTheScenarioRunIsItsOwnComponent(t *testing.T) {
	document := spec(t)

	mine := runSchema(t, document, "/api/v1/scenario/runs/{id}", "200")
	theirs := runSchema(t, document, "/api/v1/contingency/runs/{id}", "200")

	if mine == theirs {
		t.Fatalf("both run endpoints document component %q; one of them is "+
			"describing the other extension's type", mine)
	}

	// Named rather than merely distinct. A namespaced fallback would also be
	// distinct, and would rename whichever type lost the race.
	if theirs != "Run" {
		t.Errorf("contingency's sweep run is documented as %q; it held \"Run\" "+
			"before this extension existed, and a generated client is built "+
			"against that name", theirs)
	}

	// The component this extension's endpoint points at really describes a
	// scenario run. A shared component would still resolve — to the wrong
	// shape — so the name alone is not the assertion.
	properties := dig(t, document, "components", "schemas", mine, "properties")

	for _, want := range []string{"manifest", "scenario_id", "weather_rated", "failure"} {
		if _, present := properties[want]; !present {
			t.Errorf("component %q has no %q; it is not describing a scenario run", mine, want)
		}
	}
}

// Every component this extension publishes still describes this extension's
// type.
//
// The test above guards one component by comparing two endpoints' refs. That
// catches the collision it was written for and no other: this extension
// publishes six component names, and `Manifest` and `Failure` are exactly the
// kind of bare noun a second extension reaches for. Forge keys a component by
// bare Go type name, so the loser's endpoints would document the winner's
// shape and still resolve — the name alone proves nothing, which is why each
// entry below names fields only this extension's type has.
//
// Scoped to this extension rather than swept over the tree. A repo-wide guard
// would fail today on thirteen pre-existing duplicates and be turned off; one
// that covers what this extension is responsible for is a guard that survives.
//
// What it therefore does NOT catch: spec() builds its document from the
// contingency and scenario controllers alone, so a THIRD extension shipping
// its own Manifest or Failure would collide in the real application document
// while this test stayed green. Register new controllers here as they ship —
// the harness is the limit, not the assertions.
func TestEveryComponentThisExtensionPublishesIsItsOwn(t *testing.T) {
	document := spec(t)

	// Policy, Status and Step are absent deliberately: Forge inlines a string
	// enum rather than making it a component, so there is nothing to collide.
	mine := map[string][]string{
		"Manifest":             {"model_hash", "weather_cycle", "valid_at", "policy", "seed"},
		"Failure":              {"step", "reason"},
		"Scenario":             {"id", "name", "manifest", "created_at"},
		"ScenarioRun":          {"scenario_id", "manifest", "status", "queued_at"},
		"scenariosResponse":    {"scenarios"},
		"scenarioRunsResponse": {"runs"},
	}

	for component, fields := range mine {
		// dig fails when the component is missing at all, which is the other
		// half of this: a rename that moved one of these under a new name
		// would leave a generated client with no type for it.
		properties := dig(t, document, "components", "schemas", component, "properties")

		for _, want := range fields {
			if _, present := properties[want]; !present {
				t.Errorf("component %q has no %q; it is documenting some other "+
					"extension's type of the same name", component, want)
			}
		}
	}
}

// Every component this extension's paths reference is one the map above
// covers.
//
// The map is a list someone has to remember to extend, and the two collisions
// this file was written for were both types nobody thought to check. This is
// the half that does not depend on remembering: it walks the extension's own
// paths, collects every component they point at, and fails on one that is not
// accounted for — either in the map, or as a type this extension deliberately
// borrows from elsewhere.
//
// The immediate case is the run index. Both run listings answer with
// scenarioRunsResponse rather than a second wrapper of the same shape; a
// `recentRunsResponse` added later would be a new component with a new name,
// and this is what would say so.
func TestNoScenarioPathReferencesAnUnaccountedComponent(t *testing.T) {
	document := spec(t)

	accounted := map[string]bool{
		"Manifest": true, "Failure": true, "Scenario": true, "ScenarioRun": true,
		"scenariosResponse": true, "scenarioRunsResponse": true,
	}

	paths, ok := document["paths"].(map[string]any)
	if !ok {
		t.Fatal("the specification has no paths")
	}

	for path, item := range paths {
		if !strings.HasPrefix(path, "/api/v1/scenario/") {
			continue
		}

		for _, name := range componentsUnder(item) {
			if !accounted[name] {
				t.Errorf("%s references component %q, which no assertion in this "+
					"file covers; add it to the map above with fields only this "+
					"extension's type has, or answer with a component already there",
					path, name)
			}
		}
	}
}

// componentsUnder collects every schema component named anywhere beneath a
// decoded JSON value, by walking for $ref rather than by knowing the shape of
// an OpenAPI path item — a request body, a response and a nested property all
// reference the same way, and a walk cannot miss the one that moves.
func componentsUnder(node any) []string {
	const prefix = "#/components/schemas/"

	var found []string

	switch typed := node.(type) {
	case map[string]any:
		for key, value := range typed {
			if ref, ok := value.(string); key == "$ref" && ok {
				if name, cut := strings.CutPrefix(ref, prefix); cut {
					found = append(found, name)
				}

				continue
			}

			found = append(found, componentsUnder(value)...)
		}
	case []any:
		for _, value := range typed {
			found = append(found, componentsUnder(value)...)
		}
	}

	return found
}

// The run index answers with the component the per-scenario listing already
// publishes.
//
// Not a style preference. A second wrapper holding one `runs` field would put
// a byte-identical component in the specification and a byte-identical type in
// every generated client — two names for one shape, which is the collision
// problem inverted and just as much work for a reader to undo. The two
// endpoints differ in which runs they select, not in what a run list is.
func TestBothRunListingsAnswerWithOneComponent(t *testing.T) {
	document := spec(t)

	index := runSchema(t, document, "/api/v1/scenario/runs", "200")
	perScenario := runSchema(t, document, "/api/v1/scenario/scenarios/{id}/runs", "200")

	if index != perScenario {
		t.Fatalf("the run index answers with %q and the per-scenario listing with "+
			"%q; one shape published under two names is two types in every "+
			"generated client", index, perScenario)
	}

	if index != "scenarioRunsResponse" {
		t.Errorf("the run listings answer with %q; scenarioRunsResponse is the "+
			"name the other assertions in this file guard", index)
	}
}

// A field the encoder omits must not be documented as always present.
//
// Absent means unknown on this surface: a queued run has no child ids, no
// start and no finish, and the encoder omits all four. Forge decides
// requiredness from `omitempty` and pointer-ness and knows nothing of
// `omitzero`, so without the optional tags on those fields the specification
// promises four values that are missing from every queued run — and a client
// generated from it would type them as always there.
func TestTheSpecificationDoesNotPromiseWhatAQueuedRunOmits(t *testing.T) {
	document := spec(t)

	mine := runSchema(t, document, "/api/v1/scenario/runs/{id}", "200")

	component := dig(t, document, "components", "schemas", mine)

	required := make([]string, 0)

	if listed, ok := component["required"].([]any); ok {
		for _, one := range listed {
			if name, ok := one.(string); ok {
				required = append(required, name)
			}
		}
	}

	for _, absentable := range []string{
		"sweep_run_id", "cascade_run_id", "started_at", "finished_at",
		"failure", "weather_rated", "branch_count", "base_voltage_fallback",
	} {
		if slices.Contains(required, absentable) {
			t.Errorf("the specification marks %q required, and a queued run has none of it",
				absentable)
		}
	}

	// The fields a run always has must still be required, or the tag was
	// applied with a broom rather than a scalpel.
	for _, always := range []string{"id", "scenario_id", "manifest", "status", "queued_at"} {
		if !slices.Contains(required, always) {
			t.Errorf("the specification makes %q optional; every run has one", always)
		}
	}
}
