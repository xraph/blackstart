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
		"failure", "weather_rated", "branch_count",
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
