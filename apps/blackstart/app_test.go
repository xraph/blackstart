package blackstart_test

// What this file guards is a class of bug rather than one wiring line: an
// extension whose entities are declared and whose tables are never created.
// The scenario store existed for a week in exactly that state — its Group()
// and Contributor{} referenced only from their own package's tests — and
// nothing failed, because a migration group nobody hands to the orchestrator
// is not an error, it is silence.

import (
	"slices"
	"testing"

	groveext "github.com/xraph/grove/extension"

	"github.com/xraph/blackstart/apps/blackstart"
	"github.com/xraph/blackstart/internal/server"
)

// fabricUsesFabriqsOwnGroup is the one contributor whose tables are not
// created by a group of its own name.
//
// The fabric extension contributes the outbox and projection entities Fabriq
// itself declares, and Fabriq's migrations create them — so its group is
// named for the library rather than for the extension. Written down as a
// mapping rather than skipped, because "fabric has no group" and "fabric's
// group is called something else" are different facts and only one of them
// is safe.
var fabricUsesFabriqsOwnGroup = map[string]string{"fabric": "fabriq"}

// Every extension that declares entities has a migration group handed to the
// orchestrator.
//
// Declaring entities and registering a group are two separate lines in two
// separate files, and nothing in the platform connects them: an entity spec
// with no group produces a registry that validates, a fabriq that opens, and
// a store whose every write fails against a table that was never created.
// Grove's own note is why they must reach ONE orchestrator — DependsOn is a
// silent no-op across several — so this reads the groups back off the one
// that has them.
func TestEveryContributorHasItsMigrations(t *testing.T) {
	t.Parallel()

	app, err := server.Build(server.Config{
		DatabaseDSN: "postgres://localhost/blackstart",
		// With a solver, or the solver-backed extensions are not registered
		// and the three contributors behind them go unchecked.
		SolverAddress: "localhost:50051",
	}, blackstart.Contributors...)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	registered := make([]string, 0)

	for _, extension := range app.Extensions() {
		grove, ok := extension.(*groveext.Extension)
		if !ok {
			continue
		}

		for _, group := range grove.MigrationGroups() {
			registered = append(registered, group.Name())
		}
	}

	if len(registered) == 0 {
		t.Fatal("no migration groups are registered at all")
	}

	for _, contributor := range blackstart.Contributors {
		want := contributor.ExtensionName()
		if mapped, remapped := fabricUsesFabriqsOwnGroup[want]; remapped {
			want = mapped
		}

		if !slices.Contains(registered, want) {
			t.Errorf("%s declares entities but no %q migration group is registered; "+
				"its tables would never be created", contributor.ExtensionName(), want)
		}
	}
}

// The scenario extension is in both lists.
//
// Asserted by name as well as by the rule above, because the rule passes
// vacuously for an extension that is in neither: a store with no contributor
// and no group is invisible to it, which is precisely the state this
// extension shipped six tasks in.
func TestTheScenarioStoreIsRegistered(t *testing.T) {
	t.Parallel()

	names := make([]string, 0, len(blackstart.Contributors))
	for _, contributor := range blackstart.Contributors {
		names = append(names, contributor.ExtensionName())
	}

	if !slices.Contains(names, "scenario") {
		t.Errorf("the contributor list %v does not declare scenario's entities", names)
	}
}
