package memory

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/xraph/blackstart/extensions/scenario/scenario"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// Runs returns the run store.
func (s *Store) Runs() scenario.RunStore { return (*runs)(s) }

type runs Store

// Create records a run, rejecting a duplicate identifier rather than
// overwriting: runs are append-only, and an overwrite would replace a
// lifecycle someone may already have observed.
func (s *runs) Create(_ context.Context, in scenario.Run) (scenario.Run, error) {
	if err := in.Validate(); err != nil {
		return scenario.Run{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.runs[in.ID]; exists {
		return scenario.Run{}, fmt.Errorf(
			"%w: run %s already exists; runs are append-only", bserr.ErrConflict, in.ID)
	}

	s.runs[in.ID] = in
	s.runOrder = append(s.runOrder, in.ID)

	return in, nil
}

// Get retrieves one run.
func (s *runs) Get(_ context.Context, runID id.ID) (scenario.Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	found, ok := s.runs[runID]
	if !ok {
		return scenario.Run{}, fmt.Errorf("%w: run %s", bserr.ErrNotFound, runID)
	}

	return found, nil
}

// ListByScenario returns runs for one scenario, most recent first.
//
// A limit of zero or less returns everything, which is what a CLI wants.
func (s *runs) ListByScenario(
	_ context.Context, scenarioID id.ID, limit int,
) ([]scenario.Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []scenario.Run

	for _, key := range s.runOrder {
		if found := s.runs[key]; found.ScenarioID == scenarioID {
			out = append(out, found)
		}
	}

	slices.Reverse(out)

	// Ties on QueuedAt break on identifier, which is chronologically
	// sortable, so two runs queued in the same instant still order
	// reproducibly.
	slices.SortStableFunc(out, func(left, right scenario.Run) int {
		if by := right.QueuedAt.Compare(left.QueuedAt); by != 0 {
			return by
		}

		return cmp.Compare(right.ID.String(), left.ID.String())
	})

	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}

	return out, nil
}

// Claim takes the oldest queued run, marks it running, and returns it.
//
// "Oldest" means the smallest QueuedAt among queued runs, tie-broken on
// identifier — the same order the Fabriq store's Claim asks Postgres for
// with `ORDER BY queued_at ASC, id ASC`. It is not runOrder (insertion
// order): a run backfilled with an earlier QueuedAt after a later one was
// already created must still be claimed first, and scanning runOrder alone
// would claim the later-created one instead, silently disagreeing with the
// database-backed store about which run is "oldest".
//
// The scan for the oldest queued run, the mutation of its Status, and the
// write back to the map all happen under one Lock — not RLock — so the
// whole read-modify-write is one atomic step. Two goroutines calling Claim
// at the same instant serialize on s.mu: whichever gets the lock second sees
// the first goroutine's write already applied, so it scans past the run the
// first one just claimed instead of claiming it again. That is the property
// a sequential test cannot exercise, because a sequential test never puts
// two callers inside the critical section at once — it would pass even if
// Claim read the map, released the lock, and only then wrote the result back,
// which is exactly the race this design avoids.
func (s *runs) Claim(_ context.Context) (scenario.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var (
		oldestKey id.ID
		oldest    scenario.Run
		found     bool
	)

	for _, key := range s.runOrder {
		candidate := s.runs[key]
		if candidate.Status != scenario.StatusQueued {
			continue
		}

		if !found || olderCandidate(candidate, oldest) {
			oldestKey, oldest, found = key, candidate, true
		}
	}

	if !found {
		return scenario.Run{}, fmt.Errorf("%w: no run is queued", bserr.ErrNotFound)
	}

	oldest.Status = scenario.StatusRunning
	oldest.StartedAt = time.Now().UTC()
	s.runs[oldestKey] = oldest

	return oldest, nil
}

// Finish records a run's terminal state.
//
// The status check reads and writes under one Lock for the reason Claim does:
// two workers that both believed they owned one run would otherwise both
// write their own answer over it, and the second would win silently. Here the
// loser is told — a run that is no longer running is a conflict, not a no-op,
// because a worker whose result was discarded needs to know that it was.
func (s *runs) Finish(_ context.Context, in scenario.Run) (scenario.Run, error) {
	if err := in.Finishable(); err != nil {
		return scenario.Run{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.runs[in.ID]
	if !ok {
		return scenario.Run{}, fmt.Errorf("%w: run %s", bserr.ErrNotFound, in.ID)
	}

	if existing.Status != scenario.StatusRunning {
		return scenario.Run{}, fmt.Errorf(
			"%w: run %s is %s, not running; only a claimed run can finish",
			bserr.ErrConflict, in.ID, existing.Status)
	}

	// The frozen fields come from the stored run, never from the argument.
	//
	// Finish writes a whole record, and its caller hands back a run it has
	// been holding. Today that is always Claim's own output, but a retry
	// endpoint, an HTTP-supplied run body or a stuck-run reaper would each
	// hand back something else — and a manifest arriving that way would
	// silently replace the frozen one, leaving a record that is internally
	// consistent and still looks citable. That is the second route by which a
	// frozen manifest can move, and the worse of the two: nothing detects it.
	//
	// Which scenario a run belongs to and when it was queued are frozen for
	// the same reason. Neither is an outcome of executing.
	in.ScenarioID = existing.ScenarioID
	in.Manifest = existing.Manifest
	in.QueuedAt = existing.QueuedAt

	s.runs[in.ID] = in

	return in, nil
}

// olderCandidate reports whether candidate should be claimed before current,
// by QueuedAt and then by identifier — the same tie-break the Fabriq store's
// `ORDER BY queued_at ASC, id ASC` applies, so the two stores agree on which
// run is claimed whenever more than one is queued at the same instant.
func olderCandidate(candidate, current scenario.Run) bool {
	if !candidate.QueuedAt.Equal(current.QueuedAt) {
		return candidate.QueuedAt.Before(current.QueuedAt)
	}

	return candidate.ID.String() < current.ID.String()
}
