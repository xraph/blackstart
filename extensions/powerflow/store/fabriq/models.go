// Package fabriq is the Fabriq-backed implementation of the power-flow store.
//
// Results are durable rather than in-memory because a number that vanishes
// when the process exits cannot be cited. Every figure this platform produces
// has to be traceable to the dataset version, solver version and code commit
// behind it, and a solution nobody can retrieve satisfies none of that.
package fabriq

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/xraph/grove"

	"github.com/xraph/blackstart/extensions/powerflow/solution"
	"github.com/xraph/blackstart/pkg/id"
)

// Table and entity names.
const (
	SolutionTable  = "blackstart_powerflow_solution"
	SolutionEntity = "powerflow_solution"
)

// solutionRow is the persistence shape of a solved power flow.
//
// The per-bus and per-branch answers are one JSON column each rather than
// child tables. They are written once and read whole — nothing ever asks for
// one bus's voltage without the rest of the solution — and a child table would
// mean 2000 inserts per solve to support a query nobody makes.
type solutionRow struct {
	grove.BaseModel `grove:"table:blackstart_powerflow_solution"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`

	ModelHash              string    `grove:"model_hash,notnull"`
	Method                 string    `grove:"method,notnull"`
	SolverVersion          string    `grove:"solver_version,notnull"`
	EnforcedReactiveLimits bool      `grove:"enforced_reactive_limits,notnull"`
	Iterations             int64     `grove:"iterations,notnull"`
	Residual               float64   `grove:"residual,notnull"`
	SolvedAt               time.Time `grove:"solved_at,notnull"`

	Buses             []byte `grove:"buses,notnull"`
	Branches          []byte `grove:"branches,notnull"`
	BindingGenerators []byte `grove:"binding_generators,notnull"`
}

// fromDomain maps a solution onto the persistence shape.
func fromDomain(in solution.Solution) (solutionRow, error) {
	buses, err := json.Marshal(in.Buses)
	if err != nil {
		return solutionRow{}, fmt.Errorf("powerflow: encoding bus results: %w", err)
	}

	branches, err := json.Marshal(in.Branches)
	if err != nil {
		return solutionRow{}, fmt.Errorf("powerflow: encoding branch results: %w", err)
	}

	// Marshaled even when empty, so the column is always valid JSON and a
	// reader never has to distinguish "no generators bound" from "nothing
	// written".
	binding, err := json.Marshal(in.BindingGenerators)
	if err != nil {
		return solutionRow{}, fmt.Errorf("powerflow: encoding binding generators: %w", err)
	}

	return solutionRow{
		ID:                     in.ID.String(),
		ModelHash:              in.ModelHash,
		Method:                 string(in.Method),
		SolverVersion:          in.SolverVersion,
		EnforcedReactiveLimits: in.EnforcedReactiveLimits,
		Iterations:             int64(in.Iterations),
		Residual:               in.Residual,
		SolvedAt:               in.SolvedAt,
		Buses:                  buses,
		Branches:               branches,
		BindingGenerators:      binding,
	}, nil
}

// toDomain maps a stored row back.
func (r solutionRow) toDomain() (solution.Solution, error) {
	solutionID, err := id.Parse(r.ID)
	if err != nil {
		return solution.Solution{}, fmt.Errorf("powerflow: stored id %q: %w", r.ID, err)
	}

	out := solution.Solution{
		ID:                     solutionID,
		ModelHash:              r.ModelHash,
		Method:                 solution.Method(r.Method),
		SolverVersion:          r.SolverVersion,
		EnforcedReactiveLimits: r.EnforcedReactiveLimits,
		Iterations:             uint32(r.Iterations),
		Residual:               r.Residual,
		SolvedAt:               r.SolvedAt,
	}

	for _, decode := range []struct {
		name string
		body []byte
		into any
	}{
		{"bus results", r.Buses, &out.Buses},
		{"branch results", r.Branches, &out.Branches},
		{"binding generators", r.BindingGenerators, &out.BindingGenerators},
	} {
		if len(decode.body) == 0 {
			continue
		}

		if err := json.Unmarshal(decode.body, decode.into); err != nil {
			return solution.Solution{}, fmt.Errorf(
				"powerflow: solution %s: decoding %s: %w", r.ID, decode.name, err)
		}
	}

	return out, nil
}
