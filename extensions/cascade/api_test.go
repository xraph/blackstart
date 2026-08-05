package cascade_test

// Exercised over a real Forge router, like scenario's api_test.go: what these
// tests read is what an HTTP client would receive, not a Go value a handler
// returned. The fixtures and engine wiring reuse extent_test.go's — tripped,
// extentFixture, extentEngine — because the extent endpoint is a thin wire
// over the same Engine.Extent this package already tests directly.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xraph/forge"

	"github.com/xraph/blackstart/extensions/cascade"
	cid "github.com/xraph/blackstart/extensions/cascade/id"
	"github.com/xraph/blackstart/extensions/cascade/run"
)

// extentSurface brings up a real Forge router over an engine wired for
// Extent, so the endpoint tests see what a caller would.
type extentSurface struct {
	app   forge.App
	runID string
}

func newExtentSurface(t *testing.T, fixture extentFixture, events []run.Event) *extentSurface {
	t.Helper()

	engine, runID := extentEngine(t, fixture, events)

	app := forge.New(forge.WithAppName("cascade-extent-test"))

	if err := app.RegisterController(cascade.NewController(engine, nil)); err != nil {
		t.Fatalf("RegisterController: %v", err)
	}

	if err := app.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	t.Cleanup(func() { _ = app.Stop(context.WithoutCancel(t.Context())) })

	return &extentSurface{app: app, runID: runID.String()}
}

func (s *extentSurface) do(path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	s.app.Router().ServeHTTP(rec, req)

	return rec
}

// extentWire is the shape the endpoint is expected to answer with — a
// standalone copy of the response DTO rather than an import of an unexported
// type, the same reason scenario's tests decode into the package's own
// exported types where they can and a local struct where they can't.
type extentPoint struct {
	Lon       float64 `json:"lon"`
	Lat       float64 `json:"lat"`
	Elevation float64 `json:"elevation"`
}

type extentWire struct {
	RunID      string          `json:"run_id"`
	Area       [][]extentPoint `json:"area"`
	HalfWidthM float64         `json:"half_width_m"`
	Resolved   int             `json:"resolved"`
	Untraced   int             `json:"untraced"`
	Unresolved []string        `json:"unresolved"`
}

// The route exists beside the run it belongs to, and answers a run that was
// actually recorded.
func TestExtentEndpointServesARecordedRun(t *testing.T) {
	fixture := tripped(true)
	events := []run.Event{{Kind: run.EventTrip, Element: "branch-0"}}

	s := newExtentSurface(t, fixture, events)

	rec := s.do("/api/v1/cascade/runs/" + s.runID + "/extent?half_width_m=25")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var got extentWire
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding the response: %v; body: %s", err, rec.Body.String())
	}

	if got.Resolved != 1 {
		t.Errorf("resolved = %d, want 1", got.Resolved)
	}
}

// A missing half_width_m must not become zero — a buffer of no width around
// every conductor would silently answer every request with empty ground.
// It falls back to the endpoint's stated default of 500, and that default
// comes back in the response so a caller never has to guess what was used.
func TestExtentDefaultsHalfWidthWhenMissing(t *testing.T) {
	fixture := tripped(true)
	events := []run.Event{{Kind: run.EventTrip, Element: "branch-0"}}

	s := newExtentSurface(t, fixture, events)

	rec := s.do("/api/v1/cascade/runs/" + s.runID + "/extent")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var got extentWire
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding the response: %v; body: %s", err, rec.Body.String())
	}

	if got.HalfWidthM != 500 {
		t.Errorf("half_width_m = %v, want the stated default of 500", got.HalfWidthM)
	}
}

// The half-width actually used comes back with the answer regardless of
// whether it was given or defaulted — a corridor's area is uninterpretable
// without knowing how wide a buffer produced it.
func TestExtentReportsTheHalfWidthItWasGiven(t *testing.T) {
	fixture := tripped(true)
	events := []run.Event{{Kind: run.EventTrip, Element: "branch-0"}}

	s := newExtentSurface(t, fixture, events)

	rec := s.do("/api/v1/cascade/runs/" + s.runID + "/extent?half_width_m=42.5")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var got extentWire
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding the response: %v; body: %s", err, rec.Body.String())
	}

	if got.HalfWidthM != 42.5 {
		t.Errorf("half_width_m = %v, want 42.5", got.HalfWidthM)
	}
}

// THE assertion for the null-wire bug: the ordinary state before `bsctl geo
// trace-model` has run is a tripped branch the model recognizes but has no
// traced route — resolved, untraced, and therefore no ground to draw. That
// used to answer `{"area":null,"unresolved":null,...}`, and
// `extent !== undefined && extent.length > 0` in the viewer's Scene threw on
// the `null` during render. Both fields must come back `[]`.
func TestExtentEndpointNeverSendsNullForAreaOrUnresolved(t *testing.T) {
	fixture := tripped(false) // no route: resolved, untraced, no ground
	events := []run.Event{{Kind: run.EventTrip, Element: "branch-0"}}

	s := newExtentSurface(t, fixture, events)

	rec := s.do("/api/v1/cascade/runs/" + s.runID + "/extent?half_width_m=25")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()

	if strings.Contains(body, `"area":null`) {
		t.Errorf("response carries \"area\":null, want []: %s", body)
	}

	if strings.Contains(body, `"unresolved":null`) {
		t.Errorf("response carries \"unresolved\":null, want []: %s", body)
	}

	// Decoding into a type whose zero value is nil would pass even if the
	// wire sent null, so the raw-bytes check above is the one that catches
	// the bug — this just confirms the shape decodes as the reviewer's own
	// repro described it: resolved 1, untraced 1, no ground.
	var got extentWire
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding the response: %v; body: %s", err, body)
	}

	if got.Resolved != 1 || got.Untraced != 1 {
		t.Fatalf("resolved=%d untraced=%d, want 1 and 1 (a resolved, untraced trip)",
			got.Resolved, got.Untraced)
	}

	if len(got.Area) != 0 {
		t.Errorf("area has %d polygons, want 0 for an untraced branch", len(got.Area))
	}
}

// A run nobody recorded is a 404 through this endpoint too, the same as
// GetRun and GetIslands: the request was well formed, and the answer is that
// there is no such thing.
func TestExtentEndpointForAnUnrecordedRunIsNotFound(t *testing.T) {
	fixture := tripped(true)

	s := newExtentSurface(t, fixture, nil)

	rec := s.do("/api/v1/cascade/runs/" + cid.NewRun().String() + "/extent")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
}
