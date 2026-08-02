package entities

import (
	"strings"
	"testing"

	"github.com/xraph/fabriq/core/registry"
	"github.com/xraph/grove"
)

// Minimal models satisfying Fabriq's structural contract: every entity must
// declare id, tenant_id and version, and the registry rejects one that does
// not.
type busModel struct {
	grove.BaseModel `grove:"table:test_buses"`

	ID        string `grove:"id,pk"`
	TenantID  string `grove:"tenant_id,notnull"`
	Version   int64  `grove:"version,notnull"`
	Name      string `grove:"name"`
	StationID string `grove:"station_id"`
}

type stationModel struct {
	grove.BaseModel `grove:"table:test_stations"`

	ID       string `grove:"id,pk"`
	TenantID string `grove:"tenant_id,notnull"`
	Version  int64  `grove:"version,notnull"`
	Name     string `grove:"name"`
}

type contributor struct {
	name  string
	specs []registry.EntitySpec
}

func (c contributor) ExtensionName() string        { return c.name }
func (c contributor) Specs() []registry.EntitySpec { return c.specs }

func topology() contributor {
	return contributor{
		name: "topology",
		specs: []registry.EntitySpec{
			{
				Name:      "station",
				Kind:      registry.KindAggregate,
				Model:     (*stationModel)(nil),
				GraphNode: "Station",
			},
			{
				Name:      "bus",
				Kind:      registry.KindAggregate,
				Model:     (*busModel)(nil),
				GraphNode: "Bus",
				Edges: []registry.EdgeSpec{
					{Field: "station_id", Rel: "LOCATED_AT", Target: "station"},
				},
			},
		},
	}
}

func TestBuildRegistersEveryContribution(t *testing.T) {
	t.Parallel()

	reg, err := Build(topology())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	for _, name := range []string{"bus", "station"} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("%q was not registered", name)
		}
	}
}

// Entity names are global to the registry, so two extensions claiming one is a
// startup failure. The message must name both, or the reader has to grep for
// the other claimant.
func TestBuildRejectsDuplicateEntityNames(t *testing.T) {
	t.Parallel()

	other := contributor{
		name: "weather",
		specs: []registry.EntitySpec{{
			Name:  "bus",
			Kind:  registry.KindAggregate,
			Model: (*busModel)(nil),
		}},
	}

	_, err := Build(topology(), other)
	if err == nil {
		t.Fatal("two extensions claimed the entity name `bus`")
	}

	for _, want := range []string{"topology", "weather", "bus"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should name %q, got %q", want, err)
		}
	}
}

// Validation runs here rather than inside fabriq.Open so the message names the
// extension responsible. An edge pointing at an unregistered entity is the
// common case: a rename on one side of a relationship.
func TestBuildReportsTheOwnerOfAnInvalidSpec(t *testing.T) {
	t.Parallel()

	broken := contributor{
		name: "topology",
		specs: []registry.EntitySpec{{
			Name:      "bus",
			Kind:      registry.KindAggregate,
			Model:     (*busModel)(nil),
			GraphNode: "Bus",
			Edges: []registry.EdgeSpec{
				{Field: "station_id", Rel: "LOCATED_AT", Target: "nonexistent"},
			},
		}},
	}

	_, err := Build(broken)
	if err == nil {
		t.Fatal("accepted an edge pointing at an unregistered entity")
	}
}

func TestBuildRejectsAnUnnamedContributor(t *testing.T) {
	t.Parallel()

	if _, err := Build(contributor{name: ""}); err == nil {
		t.Error("accepted a contributor that did not name itself")
	}
}

func TestBuildRejectsAnUnnamedEntity(t *testing.T) {
	t.Parallel()

	_, err := Build(contributor{
		name:  "topology",
		specs: []registry.EntitySpec{{Kind: registry.KindAggregate, Model: (*busModel)(nil)}},
	})
	if err == nil {
		t.Error("accepted an entity with no name")
	}
}

func TestBuildWithNoContributors(t *testing.T) {
	t.Parallel()

	reg, err := Build()
	if err != nil {
		t.Fatalf("an empty registry should be valid: %v", err)
	}

	if got := len(reg.All()); got != 0 {
		t.Errorf("an empty registry has %d entities", got)
	}
}

// Fabriq silently skips a GraphNode when no graph adapter is configured, so
// the fabric extension asserts on this list at startup. It must be complete
// and deterministic.
func TestGraphEntities(t *testing.T) {
	t.Parallel()

	reg, err := Build(topology())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	got := GraphEntities(reg)
	if len(got) != 2 {
		t.Fatalf("GraphEntities returned %v, want both entities", got)
	}

	if got[0] != "bus" || got[1] != "station" {
		t.Errorf("GraphEntities is not sorted: %v", got)
	}
}

func TestGraphEntitiesIsEmptyWithoutGraphSpecs(t *testing.T) {
	t.Parallel()

	reg, err := Build(contributor{
		name: "plain",
		specs: []registry.EntitySpec{{
			Name:  "station",
			Kind:  registry.KindAggregate,
			Model: (*stationModel)(nil),
		}},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if got := GraphEntities(reg); len(got) != 0 {
		t.Errorf("GraphEntities returned %v for a registry with no graph specs", got)
	}
}

// The inventory is printed at startup and golden-filed, so it must be stable
// across runs — map iteration order would make it useless for both.
func TestInventoryIsStableAndNamesThePlanes(t *testing.T) {
	t.Parallel()

	reg, err := Build(topology())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	first := Inventory(reg)
	if first != Inventory(reg) {
		t.Fatal("Inventory is not stable across calls")
	}

	lines := strings.Split(strings.TrimSpace(first), "\n")
	if len(lines) != 2 {
		t.Fatalf("inventory has %d lines, want 2:\n%s", len(lines), first)
	}

	// Sorted, so `bus` precedes `station`.
	if !strings.HasPrefix(lines[0], "bus\t") {
		t.Errorf("inventory is not sorted:\n%s", first)
	}

	for _, line := range lines {
		if !strings.Contains(line, "relational") {
			t.Errorf("every entity participates in the relational plane: %q", line)
		}

		if !strings.Contains(line, "graph") {
			t.Errorf("both fixtures declare a graph node: %q", line)
		}
	}
}
