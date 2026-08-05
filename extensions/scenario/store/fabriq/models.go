// Package fabriq is the Fabriq-backed implementation of the scenario store.
//
// A scenario is a definition a user authors, not derived data, so durability
// is not optional here the way it was for cascade's run — a scenario nobody
// can retrieve after a restart is not a binding anyone can rely on, and a run
// queued for a worker that has not yet claimed it must survive the worker
// dying before it does.
package fabriq

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/xraph/grove"

	sid "github.com/xraph/blackstart/extensions/scenario/id"
	"github.com/xraph/blackstart/extensions/scenario/scenario"
	"github.com/xraph/blackstart/pkg/id"
)

// Table and entity names.
const (
	ScenarioTable = "blackstart_scenario_scenario"
	RunTable      = "blackstart_scenario_run"

	ScenarioEntity = "scenario_scenario"
	RunEntity      = "scenario_run"
)

// scenarioRow is the persistence shape of a stored scenario binding.
//
// Manifest is one JSON column rather than exploded fields. It is a value
// pinned as a whole — a model hash, a weather cycle, a valid time, a hazard
// configuration, a policy, a seed — and a schema change must not be able to
// reinterpret an old scenario's manifest, the same reasoning runRow's own
// Manifest field carries below.
type scenarioRow struct {
	grove.BaseModel `grove:"table:blackstart_scenario_scenario"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	Name     string `grove:"name,notnull"`
	Manifest []byte `grove:"manifest,notnull"`

	CreatedAt time.Time `grove:"created_at,notnull"`
	UpdatedAt time.Time `grove:"updated_at,notnull"`
}

// scenarioFromDomain maps a scenario onto the persistence shape.
func scenarioFromDomain(in scenario.Scenario) (scenarioRow, error) {
	manifest, err := json.Marshal(in.Manifest)
	if err != nil {
		return scenarioRow{}, fmt.Errorf("scenario: encoding manifest: %w", err)
	}

	return scenarioRow{
		ID:        in.ID.String(),
		Name:      in.Name,
		Manifest:  manifest,
		CreatedAt: in.CreatedAt.UTC(),
		UpdatedAt: in.UpdatedAt.UTC(),
	}, nil
}

// toDomain maps a stored scenario row back.
func (r scenarioRow) toDomain() (scenario.Scenario, error) {
	scenarioID, err := sid.ParseScenario(r.ID)
	if err != nil {
		return scenario.Scenario{}, fmt.Errorf("scenario: stored id %q: %w", r.ID, err)
	}

	var manifest scenario.Manifest
	if err := json.Unmarshal(r.Manifest, &manifest); err != nil {
		return scenario.Scenario{}, fmt.Errorf(
			"scenario: %s: decoding manifest: %w", r.ID, err)
	}

	return scenario.Scenario{
		ID:        scenarioID,
		Name:      r.Name,
		Manifest:  manifest,
		CreatedAt: r.CreatedAt.UTC(),
		UpdatedAt: r.UpdatedAt.UTC(),
	}, nil
}

// runRow is the persistence shape of one scenario execution.
//
// The frozen manifest is one JSON column rather than exploded fields, because
// it is a value pinned as a whole: a run must stay reproducible from its own
// record even if the scenario it came from changed underneath it, and a
// schema change that let a column-level migration reinterpret an old run's
// manifest would defeat that.
//
// The run's own scalars stay columns — status, the child-run links, and the
// coverage counters are what a worker and a dashboard query by, and burying
// them in JSON would make "what is queued" a scan.
type runRow struct {
	grove.BaseModel `grove:"table:blackstart_scenario_run"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	ScenarioID string `grove:"scenario_id,notnull"`
	Manifest   []byte `grove:"manifest,notnull"`
	Status     string `grove:"status,notnull"`

	// Failure carries no notnull tag and is left nil rather than marshaled
	// when absent. A run that has not failed has no failure to report, which
	// is different from a failure with no step — marshaling a nil *Failure
	// would store a JSON null, a value, rather than an absent column.
	Failure []byte `grove:"failure"`

	// SweepRunID and CascadeRunID carry no notnull tag: both are absent until
	// Status reaches StatusComplete, and an empty string round-trips as the
	// zero id.ID rather than a parse failure — the same pattern
	// networkmodel's DatasetVersionID and hazard's RunID use for an optional
	// foreign identifier.
	SweepRunID   string `grove:"sweep_run_id"`
	CascadeRunID string `grove:"cascade_run_id"`

	// WeatherRated and BranchCount are pointers so the database can
	// distinguish "the rating step has not run" from "it rated zero of
	// however many" — the same distinction scenario.ScenarioRun's own fields protect.
	// A nil pointer scans to and from SQL NULL without any tag beyond the
	// column name; tagging either notnull would make "not yet rated" an
	// unstorable state.
	WeatherRated *int `grove:"weather_rated"`
	BranchCount  *int `grove:"branch_count"`

	// BaseVoltageFallback is a pointer for the same reason and against a
	// second absence the other two do not have: the column was added by
	// migration 0003, so every run stored before it reads NULL — "not
	// recorded" rather than "fell back nowhere". Defaulting it to 0 in the
	// DDL would have written that second answer over every historical row,
	// and 0 is a claim about a rating nobody made.
	BaseVoltageFallback *int `grove:"base_voltage_fallback"`

	// SolverVersion carries no notnull tag: it is read from the solver's own
	// response once the cascade step runs, so a queued or still-running run
	// has none yet.
	SolverVersion string `grove:"solver_version"`

	QueuedAt time.Time `grove:"queued_at,notnull"`

	// StartedAt and FinishedAt are nullzero: a run that has not been claimed
	// or has not reached a terminal state has no timestamp to report, and
	// writing a zero time.Time into a NOT NULL column would store a year-1
	// timestamp that reads as data rather than as absence — the same
	// reasoning provenance's runModel.FinishedAt documents.
	StartedAt  time.Time `grove:"started_at,nullzero"`
	FinishedAt time.Time `grove:"finished_at,nullzero"`
}

// runFromDomain maps a run onto the persistence shape.
func runFromDomain(in scenario.ScenarioRun) (runRow, error) {
	manifest, err := json.Marshal(in.Manifest)
	if err != nil {
		return runRow{}, fmt.Errorf("scenario: encoding manifest: %w", err)
	}

	row := runRow{
		ID:                  in.ID.String(),
		ScenarioID:          in.ScenarioID.String(),
		Manifest:            manifest,
		Status:              string(in.Status),
		WeatherRated:        in.WeatherRated,
		BranchCount:         in.BranchCount,
		BaseVoltageFallback: in.BaseVoltageFallback,
		SolverVersion:       in.SolverVersion,
		QueuedAt:            in.QueuedAt.UTC(),
		StartedAt:           in.StartedAt.UTC(),
		FinishedAt:          in.FinishedAt.UTC(),
	}

	if in.Failure != nil {
		failure, err := json.Marshal(in.Failure)
		if err != nil {
			return runRow{}, fmt.Errorf("scenario: encoding failure: %w", err)
		}

		row.Failure = failure
	}

	if !in.SweepRunID.IsZero() {
		row.SweepRunID = in.SweepRunID.String()
	}

	if !in.CascadeRunID.IsZero() {
		row.CascadeRunID = in.CascadeRunID.String()
	}

	return row, nil
}

// toDomain maps a stored run row back.
func (r runRow) toDomain() (scenario.ScenarioRun, error) {
	runID, err := sid.ParseRun(r.ID)
	if err != nil {
		return scenario.ScenarioRun{}, fmt.Errorf("scenario: stored run id %q: %w", r.ID, err)
	}

	scenarioID, err := sid.ParseScenario(r.ScenarioID)
	if err != nil {
		return scenario.ScenarioRun{}, fmt.Errorf(
			"scenario: run %s: stored scenario id %q: %w", r.ID, r.ScenarioID, err)
	}

	var manifest scenario.Manifest
	if err := json.Unmarshal(r.Manifest, &manifest); err != nil {
		return scenario.ScenarioRun{}, fmt.Errorf(
			"scenario: run %s: decoding manifest: %w", r.ID, err)
	}

	out := scenario.ScenarioRun{
		ID:                  runID,
		ScenarioID:          scenarioID,
		Manifest:            manifest,
		Status:              scenario.Status(r.Status),
		WeatherRated:        r.WeatherRated,
		BranchCount:         r.BranchCount,
		BaseVoltageFallback: r.BaseVoltageFallback,
		SolverVersion:       r.SolverVersion,
		QueuedAt:            r.QueuedAt.UTC(),
	}

	if len(r.Failure) > 0 {
		var failure scenario.Failure
		if err := json.Unmarshal(r.Failure, &failure); err != nil {
			return scenario.ScenarioRun{}, fmt.Errorf(
				"scenario: run %s: decoding failure: %w", r.ID, err)
		}

		out.Failure = &failure
	}

	// SweepRunID and CascadeRunID name identifiers minted by other
	// extensions — contingency's sweep run and cascade's run — not this
	// extension's own run prefix, so they are parsed with the generic
	// parser rather than sid.ParseRun.
	if r.SweepRunID != "" {
		sweepRunID, err := id.Parse(r.SweepRunID)
		if err != nil {
			return scenario.ScenarioRun{}, fmt.Errorf(
				"scenario: run %s: stored sweep run id %q: %w", r.ID, r.SweepRunID, err)
		}

		out.SweepRunID = sweepRunID
	}

	if r.CascadeRunID != "" {
		cascadeRunID, err := id.Parse(r.CascadeRunID)
		if err != nil {
			return scenario.ScenarioRun{}, fmt.Errorf(
				"scenario: run %s: stored cascade run id %q: %w", r.ID, r.CascadeRunID, err)
		}

		out.CascadeRunID = cascadeRunID
	}

	if !r.StartedAt.IsZero() {
		out.StartedAt = r.StartedAt.UTC()
	}

	if !r.FinishedAt.IsZero() {
		out.FinishedAt = r.FinishedAt.UTC()
	}

	return out, nil
}
