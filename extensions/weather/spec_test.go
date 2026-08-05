package weather_test

// What the generated OpenAPI document promises about this extension's types.
//
// Separate from the runtime tests because the subject is different: these
// assert the CONTRACT a client is generated from, and the way it has gone
// wrong here is a field the encoder omits being declared as always present.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/xraph/forge"

	"github.com/xraph/blackstart/extensions/weather"
)

// spec returns the OpenAPI document Forge generates for this extension's
// controller.
func spec(t *testing.T) map[string]any {
	t.Helper()

	app := forge.New(
		forge.WithAppName("weather-spec-test"),
		forge.WithAppRouterOptions(forge.WithOpenAPI(forge.OpenAPIConfig{
			Title:       "weather spec test",
			Version:     "0",
			SpecPath:    "/openapi.json",
			SpecEnabled: true,
		})),
	)

	if err := app.RegisterController(weather.NewController(nil, nil)); err != nil {
		t.Fatalf("registering the weather controller: %v", err)
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

// requiredOf reads one component's required array, failing rather than
// returning a nil a later assertion would have to interpret.
func requiredOf(t *testing.T, document map[string]any, component string) []string {
	t.Helper()

	schemas, ok := document["components"].(map[string]any)
	if !ok {
		t.Fatal("the specification has no components")
	}

	all, ok := schemas["schemas"].(map[string]any)
	if !ok {
		t.Fatal("the specification has no component schemas")
	}

	schema, ok := all[component].(map[string]any)
	if !ok {
		t.Fatalf("the specification has no %s component; the names it does "+
			"have are %v", component, strings.Join(names(all), ", "))
	}

	raw, ok := schema["required"].([]any)
	if !ok {
		// No required array at all means nothing is promised, which satisfies
		// every assertion below.
		return nil
	}

	out := make([]string, 0, len(raw))

	for _, each := range raw {
		if name, ok := each.(string); ok {
			out = append(out, name)
		}
	}

	return out
}

func names(schemas map[string]any) []string {
	out := make([]string, 0, len(schemas))
	for name := range schemas {
		out = append(out, name)
	}

	slices.Sort(out)

	return out
}

// A grid that was never sampled omits sampled_at, so the contract must not
// promise it.
//
// Forge builds the required array from `omitempty` and pointer-ness and does
// NOT read `omitzero`, so a field tagged omitzero alone is published as
// required while the encoder omits it. A generated TypeScript client then
// types sampled_at as a plain string, and `new Date(grid.sampled_at)` yields
// Invalid Date on exactly the grids that were never sampled — which are the
// grids the field exists to identify.
//
// Removing optional:"true" from Grid.SampledAt makes this fail.
func TestAnUnsampledGridIsNotPromisedASampleTime(t *testing.T) {
	required := requiredOf(t, spec(t), "Grid")

	if slices.Contains(required, "sampled_at") {
		t.Errorf("Grid declares sampled_at required, but an unsampled grid "+
			"omits it — required is %v", required)
	}
}

// The counterpart, so the test above cannot pass by the component losing its
// required array altogether.
//
// covered and total are always encoded — they are plain ints with no omit
// tag — so a contract that stopped promising them would be describing a
// different type, and this test would be the one to say so.
func TestAGridStillPromisesWhatItAlwaysEncodes(t *testing.T) {
	required := requiredOf(t, spec(t), "Grid")

	for _, want := range []string{"covered", "total"} {
		if !slices.Contains(required, want) {
			t.Errorf("Grid does not declare %q required, though every grid "+
				"encodes it — required is %v", want, required)
		}
	}
}
