package scenario

// The run half of the surface's tests. Splits from api_test.go the way
// api_runs.go splits from api.go; the recording store and the surface harness
// both live over there.

import (
	"encoding/json"
	"net/http"
	"testing"

	scnid "github.com/xraph/blackstart/extensions/scenario/id"
	scn "github.com/xraph/blackstart/extensions/scenario/scenario"
	"github.com/xraph/blackstart/pkg/id"
)

// --- Bounding the list ---

// seededRuns is how many runs the listing test queues before reading them
// back.
//
// More than one, and that is the whole point. A bound is two mistakes away
// from correct in opposite directions — too loose is "everything", too tight
// silently drops rows — and a store holding a single run cannot tell them
// apart: every assertion about the limit that reached the store still holds
// when the bound is 1 and the caller asked for sixty. Three is the smallest
// number that makes a wrongly-small bound visible in the response body.
const seededRuns = 3

// A scenario's run list may never ask a store for everything, and must still
// return what it was asked for.
//
// The first half is the rule TestListingScenariosNeverAsksAStoreForEverything
// states: the store honors a non-positive limit as "everything", and this
// surface is the caller that knows it never wants that. The second half is
// what stops that rule from being satisfied by a bound so tight it answers
// nothing — the failure mode a bound introduces, checked in the same place
// the bound is.
func TestListingRunsNeverAsksAStoreForEverything(t *testing.T) {
	for _, query := range []string{"", "?limit=0", "?limit=-1", "?limit=999999"} {
		s := newSurface(t)
		defined := s.define(t)

		queued := make(map[id.ID]bool, seededRuns)

		for range seededRuns {
			run, err := s.engine.Enqueue(t.Context(), defined.ID)
			if err != nil {
				t.Fatalf("Enqueue: %v", err)
			}

			queued[run.ID] = true
		}

		path := "/api/v1/scenario/scenarios/" + defined.ID.String() + "/runs" + query

		rec := s.do(http.MethodGet, path, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("listing%s: status %d; body: %s", query, rec.Code, rec.Body.String())
		}

		assertBounded(t, query, s.store.runLimits)

		var got scenarioRunsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decoding the listing: %v; body: %s", err, rec.Body.String())
		}

		if len(got.Runs) != seededRuns {
			t.Errorf("listing%s returned %d runs, want the %d that were queued; "+
				"a bound tight enough to drop them satisfies every check above",
				query, len(got.Runs), seededRuns)
		}

		for _, run := range got.Runs {
			if !queued[run.ID] {
				t.Errorf("listing%s returned run %s, which was never queued",
					query, run.ID)
			}

			delete(queued, run.ID)
		}

		if len(queued) > 0 {
			t.Errorf("listing%s never returned %d of the queued runs", query, len(queued))
		}
	}
}

// --- Absent means unknown ---

// A queued run publishes none of the fields it has not observed.
//
// This is the invariant four tasks have protected, at the one boundary that
// can destroy it. WeatherRated and BranchCount are *int precisely so that
// "rated zero of two thousand" — a real and alarming finding — reads
// differently from "has not looked yet". An encoding that turned either into
// 0, or Failure into an empty object, or an unset child id into "", would
// collapse the two into one everywhere a console or a generated client reads
// this response.
func TestAQueuedRunPublishesNothingItHasNotObserved(t *testing.T) {
	s := newSurface(t)
	defined := s.define(t)

	rec := s.do(http.MethodPost,
		"/api/v1/scenario/scenarios/"+defined.ID.String()+"/runs", "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d, want 202; body: %s", rec.Code, rec.Body.String())
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decoding the response: %v; body: %s", err, rec.Body.String())
	}

	for _, key := range []string{
		"weather_rated", "branch_count", "failure",
		"sweep_run_id", "cascade_run_id", "started_at", "finished_at",
	} {
		if value, present := raw[key]; present {
			t.Errorf("a queued run published %q as %s; absent is what says "+
				"'not observed', and any value here says something was", key, value)
		}
	}

	if string(raw["status"]) != `"queued"` {
		t.Errorf("status %s, want \"queued\"", raw["status"])
	}
}

// A rating count of zero is published as zero rather than omitted. The
// pointer is what makes that possible, and a run that rated none of two
// thousand branches is the finding this pair exists to surface.
func TestARunThatRatedNothingPublishesZeroRatherThanNothing(t *testing.T) {
	s := newSurface(t)

	run := s.finished(t, func(in *scn.ScenarioRun) {
		none, total := 0, 2000
		in.Status = scn.StatusFailed
		in.Failure = &scn.Failure{Step: scn.StepRatings, Reason: "no branch could be rated"}
		in.WeatherRated, in.BranchCount = &none, &total
	})

	rec := s.do(http.MethodGet, "/api/v1/scenario/runs/"+run.ID.String(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decoding the response: %v; body: %s", err, rec.Body.String())
	}

	if string(raw["weather_rated"]) != "0" {
		t.Errorf("weather_rated %s, want 0; an omitted count reads as 'never looked'",
			raw["weather_rated"])
	}

	if string(raw["branch_count"]) != "2000" {
		t.Errorf("branch_count %s, want 2000", raw["branch_count"])
	}
}

// A failed run publishes the step it failed at. The pipeline has four steps
// that fail for different reasons, and "it failed" alone is not something
// anyone can act on.
func TestAFailedRunPublishesTheStepItFailedAt(t *testing.T) {
	s := newSurface(t)

	run := s.finished(t, func(in *scn.ScenarioRun) {
		in.Status = scn.StatusFailed
		in.Failure = &scn.Failure{Step: scn.StepScreen, Reason: "the solver refused"}
	})

	rec := s.do(http.MethodGet, "/api/v1/scenario/runs/"+run.ID.String(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var got scn.ScenarioRun
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding the response: %v; body: %s", err, rec.Body.String())
	}

	if got.Failure == nil || got.Failure.Step != scn.StepScreen {
		t.Fatalf("the failed run published %+v, want a failure naming the screen step",
			got.Failure)
	}

	if got.Failure.Reason == "" {
		t.Error("the failure names its step but not why")
	}
}

// finished queues a run, claims it, and finishes it in whatever terminal
// state the caller shapes — the only route to a terminal run, since Finish
// refuses anything that was not claimed.
func (s *surface) finished(
	t *testing.T, shape func(*scn.ScenarioRun),
) scn.ScenarioRun {
	t.Helper()

	defined := s.define(t)

	if _, err := s.engine.Enqueue(t.Context(), defined.ID); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	claimed, err := s.store.Runs().Claim(t.Context())
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}

	shape(&claimed)

	stored, err := s.store.Runs().Finish(t.Context(), claimed)
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}

	return stored
}

// --- Identifiers ---

// A run identifier of another type is refused rather than looked up. srun_ and
// crun_ differ by one character and name records in two different extensions.
func TestFetchingARunByAnIdentifierOfAnotherTypeIsRefused(t *testing.T) {
	s := newSurface(t)

	rec := s.do(http.MethodGet,
		"/api/v1/scenario/runs/"+scnid.NewScenario().String(), "")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
}

// Queueing a run of a scenario nobody defined is a 404 rather than a 500: the
// request was well formed and the answer is that there is no such thing.
func TestQueueingARunOfAnUndefinedScenarioIsNotFound(t *testing.T) {
	s := newSurface(t)

	rec := s.do(http.MethodPost,
		"/api/v1/scenario/scenarios/"+scnid.NewScenario().String()+"/runs", "")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
}
