package contingency_test

// What this extension publishes on the wire, as opposed to what it computes.
//
// Separate from the engine tests because the subject is different: these assert
// the CONTRACT a generated client is built from. sweep.Run carries no json
// tags, so every Go field name on it is a published key — which makes a rename
// here a wire change and makes a newly added field's key and requiredness worth
// pinning down rather than assuming.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/xraph/forge"

	"github.com/xraph/blackstart/extensions/contingency"
	cid "github.com/xraph/blackstart/extensions/contingency/id"
	"github.com/xraph/blackstart/extensions/contingency/sweep"
)

// spec brings the controller up on its own router and returns the OpenAPI
// document Forge generates for it.
func spec(t *testing.T) map[string]any {
	t.Helper()

	app := forge.New(
		forge.WithAppName("contingency-spec-test"),
		forge.WithAppRouterOptions(forge.WithOpenAPI(forge.OpenAPIConfig{
			Title:       "contingency spec test",
			Version:     "0",
			SpecPath:    "/openapi.json",
			SpecEnabled: true,
		})),
	)

	if err := app.RegisterController(contingency.NewController(nil, nil)); err != nil {
		t.Fatalf("registering the controller: %v", err)
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

// dig walks a decoded JSON document by key, failing rather than returning a nil
// a later assertion would have to interpret.
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

// required reads a component's required array as a list of names.
func required(t *testing.T, document map[string]any, component string) []string {
	t.Helper()

	schema := dig(t, document, "components", "schemas", component)

	out := make([]string, 0)

	listed, ok := schema["required"].([]any)
	if !ok {
		return out
	}

	for _, one := range listed {
		if name, ok := one.(string); ok {
			out = append(out, name)
		}
	}

	return out
}

// The canonical hash is not promised to a client that will not always get one.
//
// Every sweep recorded before the field existed omits it, and so does every
// sweep of a model networkmodel never stored.
//
// What makes this hold, measured rather than assumed: pointer-ness alone keeps
// the field out of Forge's required array. Removing `omitempty`, and removing
// it along with an `optional:"true"` tag, were both run against this test and
// both still passed — so the mutation this assertion actually guards is turning
// CanonicalHash into a plain string, which is the "absent means zero" mistake
// the whole field exists to avoid. The companion test below is the one a tag
// change does break.
func TestTheSpecificationDoesNotPromiseACanonicalHash(t *testing.T) {
	document := spec(t)

	names := required(t, document, "Run")

	if slices.Contains(names, "CanonicalHash") {
		t.Error("the specification marks CanonicalHash required; a sweep recorded " +
			"before the field existed has none, and a client generated from this " +
			"would type it as always present")
	}

	// Not applied with a broom: the hash every run does have stays required,
	// and so does the rest of the record.
	for _, always := range []string{"ID", "ModelHash", "SolverVersion", "Candidates"} {
		if !slices.Contains(names, always) {
			t.Errorf("the specification makes %q optional; every sweep has one", always)
		}
	}
}

// The published keys of a sweep, spelled out.
//
// sweep.Run has no json tags, so these ARE the Go field names — the generated
// codec maps "ModelHash" to modelHash and the console reads that. Renaming a
// field here is a wire change, and this is what says so before a client is
// regenerated against a key that moved.
func TestASweepPublishesTheKeysItsClientWasGeneratedAgainst(t *testing.T) {
	document := spec(t)

	properties := dig(t, document, "components", "schemas", "Run", "properties")

	for _, key := range []string{
		"ID", "ModelHash", "CanonicalHash", "SolverVersion",
		"Candidates", "StartedAt", "FinishedAt",
	} {
		if _, present := properties[key]; !present {
			t.Errorf("component Run has no %q; a generated client has no field for it", key)
		}
	}
}

// Absent on the record must mean absent on the wire.
//
// A key present as null, or as "", would put a value in front of a reader that
// nobody measured. Dropping `omitempty` from sweep.Run.CanonicalHash makes the
// first half fail with `"CanonicalHash":null` — run, and it does.
//
//nolint:musttag // sweep.Run's untagged fields ARE its wire keys; see the type.
func TestAnAbsentCanonicalHashIsAbsentFromTheJSON(t *testing.T) {
	without, err := json.Marshal(sweep.Run{
		ID: cid.NewRun(), ModelHash: "protobuf-side", SolverVersion: "0.1.0",
	})
	if err != nil {
		t.Fatalf("encoding a run with no canonical hash: %v", err)
	}

	if strings.Contains(string(without), "CanonicalHash") {
		t.Errorf("a run that never learned its canonical hash publishes one: %s", without)
	}

	canonical := "canonical-side"

	with, err := json.Marshal(sweep.Run{
		ID: cid.NewRun(), ModelHash: "protobuf-side",
		CanonicalHash: &canonical, SolverVersion: "0.1.0",
	})
	if err != nil {
		t.Fatalf("encoding a run with a canonical hash: %v", err)
	}

	if !strings.Contains(string(with), `"CanonicalHash":"canonical-side"`) {
		t.Errorf("a run that knows its canonical hash does not publish it: %s", with)
	}
}
