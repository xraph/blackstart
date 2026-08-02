// Package fabriq is the Fabriq-backed implementation of the contingency store.
//
// Runs are durable rather than in-memory because a finding that vanishes when
// the process exits cannot be cited, and a contingency analysis exists to be
// cited.
package fabriq

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/xraph/grove"

	"github.com/xraph/blackstart/extensions/contingency/sweep"
	"github.com/xraph/blackstart/pkg/id"
)

// Table and entity names.
const (
	RunTable  = "blackstart_contingency_run"
	RunEntity = "contingency_run"
)

// runRow is the persistence shape of a screened sweep.
//
// Candidates are one JSON column rather than a child table per contingency and
// another per violation. A sweep is written once and read whole, and a
// 3000-branch network would otherwise mean 3000 inserts plus a variable number
// of violation rows for a query nobody makes — nobody asks for one
// contingency's result without the sweep it belongs to.
type runRow struct {
	grove.BaseModel `grove:"table:blackstart_contingency_run"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	ModelHash     string    `grove:"model_hash,notnull"`
	SolverVersion string    `grove:"solver_version,notnull"`
	StartedAt     time.Time `grove:"started_at,notnull"`
	FinishedAt    time.Time `grove:"finished_at,notnull"`

	// Concerning is denormalised so "which sweeps found something" is a
	// predicate rather than a scan through every candidate array. It is
	// derived at write time from the candidates themselves, so the two cannot
	// disagree.
	Concerning int `grove:"concerning,notnull"`

	Candidates []byte `grove:"candidates,notnull"`
}

// fromDomain maps a run onto the persistence shape.
func fromDomain(in sweep.Run) (runRow, error) {
	candidates, err := json.Marshal(in.Candidates)
	if err != nil {
		return runRow{}, fmt.Errorf("contingency: encoding candidates: %w", err)
	}

	return runRow{
		ID:            in.ID.String(),
		ModelHash:     in.ModelHash,
		SolverVersion: in.SolverVersion,
		StartedAt:     in.StartedAt,
		FinishedAt:    in.FinishedAt,
		Concerning:    len(in.Concerning()),
		Candidates:    candidates,
	}, nil
}

// toDomain maps a stored row back.
func (r runRow) toDomain() (sweep.Run, error) {
	runID, err := id.Parse(r.ID)
	if err != nil {
		return sweep.Run{}, fmt.Errorf("contingency: stored id %q: %w", r.ID, err)
	}

	out := sweep.Run{
		ID:            runID,
		ModelHash:     r.ModelHash,
		SolverVersion: r.SolverVersion,
		StartedAt:     r.StartedAt,
		FinishedAt:    r.FinishedAt,
	}

	if len(r.Candidates) > 0 {
		if err := json.Unmarshal(r.Candidates, &out.Candidates); err != nil {
			return sweep.Run{}, fmt.Errorf(
				"contingency: run %s: decoding candidates: %w", r.ID, err)
		}
	}

	return out, nil
}
