// Package fabriq is the Fabriq-backed implementation of the cascade store.
//
// Runs are durable rather than in-memory for the reason contingency's are: a
// result that vanishes when the process exits cannot be cited, and a cascade
// analysis exists to be cited. It is worse here than there — a cascade names
// the load it expects to lose, which is the figure an operator would act on.
package fabriq

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/xraph/grove"

	cid "github.com/xraph/blackstart/extensions/cascade/id"
	"github.com/xraph/blackstart/extensions/cascade/run"
)

// Table and entity names.
const (
	RunTable  = "blackstart_cascade_run"
	RunEntity = "cascade_run"
)

// runRow is the persistence shape of one cascade simulation.
//
// Events and islands are JSON columns rather than child tables. A run is
// written once and read whole — the API serves a cascade, never one event of
// one — and a long cascade would otherwise mean a few hundred inserts for a
// query nobody makes. The same reasoning contingency's candidates column
// carries, and the shapes are close enough that departing from it would be a
// difference to explain rather than a decision.
//
// The scalars of the run itself stay columns. They describe the run rather
// than living inside one of its collections, and burying an outcome in JSON
// would make "which cascades collapsed" a scan.
type runRow struct {
	grove.BaseModel `grove:"table:blackstart_cascade_run"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	ModelHash string `grove:"model_hash,notnull"`

	// CanonicalHash carries no notnull tag because NULL is the only honest
	// value for a run recorded before the column existed: that run's canonical
	// hash was never observed, and an empty string would claim it was observed
	// to be blank. The pointer is what carries NULL through grove.
	CanonicalHash *string `grove:"canonical_hash"`

	SolverVersion string `grove:"solver_version,notnull"`
	Outcome       string `grove:"outcome,notnull"`

	// SBaseVA carries no notnull tag for the same reason LoadLost below does
	// not: a base of zero is what run.CascadeRun.SBaseVA's own comment and
	// engine.go's divide already treat as "the model did not state one"
	// rather than a value fabriq should refuse to store.
	SBaseVA float64 `grove:"s_base_va"`

	// LoadLost, Steps and Elapsed carry no notnull tag. A cascade that shed
	// nothing is the ordinary good case, and fabriq's notnull is stricter than
	// the column's — it refuses a zero value outright where NOT NULL DEFAULT
	// accepts it. Tagging these would make every settled run fail to store.
	LoadLost float64 `grove:"load_lost"`
	Steps    int64   `grove:"steps"`
	Elapsed  float64 `grove:"elapsed_s"`

	StartedAt  time.Time `grove:"started_at,notnull"`
	FinishedAt time.Time `grove:"finished_at,notnull"`

	Events  []byte `grove:"events,notnull"`
	Islands []byte `grove:"islands,notnull"`

	// Initiating carries no notnull tag: a cascade whose initiating outage was
	// not recorded still encodes as "[]", and fabriq would refuse that as
	// empty for a notnull field.
	Initiating []byte `grove:"initiating"`
}

// fromDomain maps a run onto the persistence shape.
func fromDomain(in run.CascadeRun) (runRow, error) {
	events, err := json.Marshal(in.Events)
	if err != nil {
		return runRow{}, fmt.Errorf("cascade: encoding events: %w", err)
	}

	islands, err := json.Marshal(in.Islands)
	if err != nil {
		return runRow{}, fmt.Errorf("cascade: encoding islands: %w", err)
	}

	initiating, err := json.Marshal(in.Initiating)
	if err != nil {
		return runRow{}, fmt.Errorf("cascade: encoding initiating outages: %w", err)
	}

	return runRow{
		ID:            in.ID.String(),
		ModelHash:     in.ModelHash,
		CanonicalHash: in.CanonicalHash,
		SolverVersion: in.SolverVersion,
		Outcome:       string(in.Outcome),
		SBaseVA:       in.SBaseVA,
		LoadLost:      in.LoadLost,
		Steps:         int64(in.Steps),
		Elapsed:       in.Elapsed,
		StartedAt:     in.StartedAt.UTC(),
		FinishedAt:    in.FinishedAt.UTC(),
		Events:        events,
		Islands:       islands,
		Initiating:    initiating,
	}, nil
}

// toDomain maps a stored row back.
func (r runRow) toDomain() (run.CascadeRun, error) {
	runID, err := cid.ParseRun(r.ID)
	if err != nil {
		return run.CascadeRun{}, fmt.Errorf("cascade: stored id %q: %w", r.ID, err)
	}

	out := run.CascadeRun{
		ID:        runID,
		ModelHash: r.ModelHash,
		// Straight across, NULL included: the round trip must not turn "never
		// recorded" into a hash of no characters.
		CanonicalHash: r.CanonicalHash,
		SolverVersion: r.SolverVersion,
		Outcome:       run.Outcome(r.Outcome),
		SBaseVA:       r.SBaseVA,
		LoadLost:      r.LoadLost,
		Steps:         uint32(max(0, r.Steps)),
		Elapsed:       r.Elapsed,
		StartedAt:     r.StartedAt.UTC(),
		FinishedAt:    r.FinishedAt.UTC(),
	}

	if len(r.Events) > 0 {
		if err := json.Unmarshal(r.Events, &out.Events); err != nil {
			return run.CascadeRun{}, fmt.Errorf(
				"cascade: run %s: decoding events: %w", r.ID, err)
		}
	}

	if len(r.Islands) > 0 {
		if err := json.Unmarshal(r.Islands, &out.Islands); err != nil {
			return run.CascadeRun{}, fmt.Errorf(
				"cascade: run %s: decoding islands: %w", r.ID, err)
		}
	}

	if len(r.Initiating) > 0 {
		if err := json.Unmarshal(r.Initiating, &out.Initiating); err != nil {
			return run.CascadeRun{}, fmt.Errorf(
				"cascade: run %s: decoding initiating outages: %w", r.ID, err)
		}
	}

	return out, nil
}
