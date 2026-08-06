package cascade_test

// What this extension publishes on the wire, as opposed to what it computes.
//
// Separate from the engine tests because the subject is different: these assert
// the CONTRACT a generated client is built from. The immediate case is
// CanonicalHash, which most recorded runs will not have — a field promised as
// always present that the encoder omits is one of the two ways this
// repository's OpenAPI surface has gone wrong before.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/xraph/forge"

	"github.com/xraph/blackstart/extensions/cascade"
	cid "github.com/xraph/blackstart/extensions/cascade/id"
	"github.com/xraph/blackstart/extensions/cascade/run"
)

// spec brings the controller up on its own router and returns the OpenAPI
// document Forge generates for it.
func spec(t *testing.T) map[string]any {
	t.Helper()

	app := forge.New(
		forge.WithAppName("cascade-spec-test"),
		forge.WithAppRouterOptions(forge.WithOpenAPI(forge.OpenAPIConfig{
			Title:       "cascade spec test",
			Version:     "0",
			SpecPath:    "/openapi.json",
			SpecEnabled: true,
		})),
	)

	if err := app.RegisterController(cascade.NewController(nil, nil)); err != nil {
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

// cascadeRunSchema is the CascadeRun component, failing rather than returning a
// nil a later assertion would have to interpret.
func cascadeRunSchema(t *testing.T, document map[string]any) map[string]any {
	t.Helper()

	schemas, ok := document["components"].(map[string]any)
	if !ok {
		t.Fatal("the specification has no components")
	}

	byName, ok := schemas["schemas"].(map[string]any)
	if !ok {
		t.Fatal("the specification has no component schemas")
	}

	schema, ok := byName["CascadeRun"].(map[string]any)
	if !ok {
		t.Fatal("the specification has no CascadeRun component")
	}

	return schema
}

// The canonical hash is not promised to a client that will not always get one.
//
// Every cascade recorded before the field existed omits it, and so does every
// cascade of a model networkmodel never stored.
//
// Measured rather than assumed: pointer-ness alone keeps a field out of Forge's
// required array, so removing `omitempty` — with or without adding an
// `optional:"true"` — was run against this and still passed. The mutation this
// guards is turning CanonicalHash into a plain string, which is the "absent
// means zero" mistake the field exists to avoid. The JSON test below is the one
// a tag change does break.
func TestTheSpecificationDoesNotPromiseACanonicalHash(t *testing.T) {
	schema := cascadeRunSchema(t, spec(t))

	names := make([]string, 0)

	if listed, ok := schema["required"].([]any); ok {
		for _, one := range listed {
			if name, ok := one.(string); ok {
				names = append(names, name)
			}
		}
	}

	if slices.Contains(names, "canonical_hash") {
		t.Error("the specification marks canonical_hash required; a cascade recorded " +
			"before the field existed has none, and a client generated from this " +
			"would type it as always present")
	}

	// Not applied with a broom: the hash every run does have stays required.
	for _, always := range []string{"model_hash", "solver_version", "id", "outcome"} {
		if !slices.Contains(names, always) {
			t.Errorf("the specification makes %q optional; every cascade has one", always)
		}
	}

	// And the field is published at all — a client with no field for it could
	// not read one even when the server sends it.
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("the CascadeRun component has no properties")
	}

	if _, present := properties["canonical_hash"]; !present {
		t.Error("component CascadeRun has no canonical_hash; a generated client " +
			"has no field for the hash a user actually holds")
	}
}

// Absent on the record must mean absent on the wire.
//
// A key present as null, or as "", would put a value in front of a reader that
// nobody measured. Dropping `omitempty` from run.CascadeRun.CanonicalHash makes
// the first half fail with `"canonical_hash":null` — run, and it does.
func TestAnAbsentCanonicalHashIsAbsentFromTheJSON(t *testing.T) {
	without, err := json.Marshal(run.CascadeRun{
		ID: cid.NewRun(), ModelHash: "protobuf-side", SolverVersion: "0.1.0",
	})
	if err != nil {
		t.Fatalf("encoding a run with no canonical hash: %v", err)
	}

	if strings.Contains(string(without), "canonical_hash") {
		t.Errorf("a run that never learned its canonical hash publishes one: %s", without)
	}

	canonical := "canonical-side"

	with, err := json.Marshal(run.CascadeRun{
		ID: cid.NewRun(), ModelHash: "protobuf-side",
		CanonicalHash: &canonical, SolverVersion: "0.1.0",
	})
	if err != nil {
		t.Fatalf("encoding a run with a canonical hash: %v", err)
	}

	if !strings.Contains(string(with), `"canonical_hash":"canonical-side"`) {
		t.Errorf("a run that knows its canonical hash does not publish it: %s", with)
	}
}
