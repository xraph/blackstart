// Package entities is the Fabriq registry composition root.
//
// It exists because of a specific interaction between Forge's lifecycle and
// Fabriq's, verified in source rather than assumed:
//
// Forge's default startup loop interleaves Register and Start per extension
// (forge/app_impl.go) — it does not register everything and then start
// everything. Fabriq validates its entity registry inside fabriq.Open, which
// runs during the fabric extension's Start. So an extension that declared its
// EntitySpec in its own Register would be doing so after Fabriq had already
// opened, and the spec would silently never take effect: a registered entity
// nobody validated, whose writes fail at the first Exec with "unknown entity".
//
// The registry is therefore assembled eagerly here, in main, before any
// extension is constructed. Each extension exports a Contributor; this package
// gathers them in one place where the full set is visible and reviewable.
package entities

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/xraph/fabriq/core/registry"
)

// Contributor is implemented by each extension's entity declaration.
//
// It is an interface rather than a bare slice so that the owning extension is
// named alongside its specs. A registry error that says "entity `bus` is
// already registered" is much less useful than one naming both claimants.
type Contributor interface {
	// ExtensionName identifies the owner, e.g. "topology".
	ExtensionName() string

	// Specs returns the entities this extension declares.
	Specs() []registry.EntitySpec
}

// Build assembles and validates the registry.
//
// Validation runs here rather than being left to fabriq.Open, so that a
// malformed spec fails at composition with a message naming the extension
// responsible, instead of surfacing from inside the fabric extension's Start
// where the owner is no longer in scope.
func Build(contributors ...Contributor) (*registry.Registry, error) {
	reg := registry.New()

	owners := make(map[string]string)

	for _, c := range contributors {
		owner := c.ExtensionName()
		if owner == "" {
			return nil, errors.New("entities: a contributor did not name itself")
		}

		for _, spec := range c.Specs() {
			if spec.Name == "" {
				return nil, fmt.Errorf(
					"entities: %s declared an entity with no name", owner)
			}

			if prev, taken := owners[spec.Name]; taken {
				return nil, fmt.Errorf(
					"entities: %s and %s both declare the entity %q; "+
						"entity names are global to the registry",
					prev, owner, spec.Name)
			}

			if err := reg.Register(spec); err != nil {
				return nil, fmt.Errorf("entities: %s: registering %q: %w",
					owner, spec.Name, err)
			}

			owners[spec.Name] = owner
		}
	}

	if err := reg.Validate(); err != nil {
		return nil, fmt.Errorf("entities: validating the registry: %w", err)
	}

	return reg, nil
}

// GraphEntities returns the names of registered entities that declare a graph
// node or edge, in sorted order.
//
// The fabric extension asserts this is empty when no graph adapter is
// configured. Fabriq does not: a spec with GraphNode set and no FalkorDB
// address is silently skipped, so the entity is simply absent from the graph
// with no error and no log line. For a project whose architectural claim is
// that one write produces a consistent graph view, that failure mode is worth
// converting into a refusal to start.
func GraphEntities(reg *registry.Registry) []string {
	var names []string

	for _, e := range reg.All() {
		if e.Spec.GraphNode != "" || e.Spec.GraphEdge != nil {
			names = append(names, e.Spec.Name)
		}
	}

	sort.Strings(names)

	return names
}

// SearchEntities returns the names of registered entities that declare a
// search index, in sorted order. The same silent-skip reasoning applies.
func SearchEntities(reg *registry.Registry) []string {
	var names []string

	for _, e := range reg.All() {
		if e.Spec.Search.Index != "" {
			names = append(names, e.Spec.Name)
		}
	}

	sort.Strings(names)

	return names
}

// Inventory renders the registered entities, one per line, with the planes
// each participates in.
//
// Printed at startup and golden-filed in tests: adding an entity to the graph
// or search projection changes what the system maintains at runtime, and that
// should appear as a reviewable diff rather than as a one-word change buried
// in a spec literal.
func Inventory(reg *registry.Registry) string {
	all := reg.All()

	names := make([]string, 0, len(all))
	byName := make(map[string]*registry.Entity, len(all))

	for _, e := range all {
		names = append(names, e.Spec.Name)
		byName[e.Spec.Name] = e
	}

	sort.Strings(names)

	var b strings.Builder

	for _, name := range names {
		e := byName[name]

		planes := []string{"relational"}
		if e.Spec.GraphNode != "" || e.Spec.GraphEdge != nil {
			planes = append(planes, "graph")
		}

		if e.Spec.Search.Index != "" {
			planes = append(planes, "search")
		}

		fmt.Fprintf(&b, "%s\t%s\n", name, strings.Join(planes, ","))
	}

	return b.String()
}
