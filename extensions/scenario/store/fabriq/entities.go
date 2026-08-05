package fabriq

import (
	"github.com/xraph/fabriq/core/registry"
)

// Contributor supplies this extension's entity specs to the composition root.
//
// Declared here, beside the models they name, rather than in the extension
// package — a spec whose Model field points at a struct three packages away is
// a spec nobody checks against its table.
type Contributor struct{}

// ExtensionName identifies the owner in registry errors.
func (Contributor) ExtensionName() string { return "scenario" }

// Specs returns the entities this extension declares.
//
// Neither is a graph node, for the reason cascade's run is not: a scenario
// names the components it binds and a run names the scenario it executed, but
// neither is a thing the graph answers "what is connected to what" about —
// that is provenance's job, and reified there already.
func (Contributor) Specs() []registry.EntitySpec {
	return []registry.EntitySpec{
		{
			Name:  ScenarioEntity,
			Kind:  registry.KindAggregate,
			Model: (*scenarioRow)(nil),
		},
		{
			Name:  RunEntity,
			Kind:  registry.KindAggregate,
			Model: (*runRow)(nil),
		},
	}
}
