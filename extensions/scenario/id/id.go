// Package id declares the scenario extension's identifier prefixes.
//
// Prefixes are registered with the central registry from init, so a collision
// with another extension stops the process at startup with both owners named
// rather than surfacing later as identifiers that parse as the wrong type.
package id

import "github.com/xraph/blackstart/pkg/id"

// Owner is the extension these prefixes belong to.
const Owner = "scenario"

const (
	// Scenario is a stored binding: a model, a forecast, a hazard
	// configuration, a policy and a seed.
	//
	// Immutable once shipped: changing it invalidates every stored identifier
	// that uses it and every provenance record naming one.
	Scenario id.Prefix = "scn"

	// Run is one execution of a scenario.
	//
	// `srun` rather than `run` because the provenance extension already owns
	// `run`, and MustRegister below would halt startup on the collision. The
	// per-extension prefix is the house pattern anyway — contingency mints
	// `crun`, cascade mints `casc`.
	//
	// Immutable once shipped: changing it invalidates every stored identifier
	// that uses it and every provenance record naming one.
	Run id.Prefix = "srun"
)

//nolint:gochecknoinits // prefix registration is init-time by design
func init() {
	id.MustRegister(Scenario, Owner, "Scenario")
	id.MustRegister(Run, Owner, "ScenarioRun")
}

// NewScenario mints a scenario identifier.
func NewScenario() id.ID { return id.MustNew(Scenario) }

// NewRun mints a run identifier.
func NewRun() id.ID { return id.MustNew(Run) }

// ParseScenario reads a scenario identifier, rejecting any other type.
func ParseScenario(s string) (id.ID, error) { return id.ParseWithPrefix(s, Scenario) }

// ParseRun reads a run identifier, rejecting any other type.
func ParseRun(s string) (id.ID, error) { return id.ParseWithPrefix(s, Run) }
