package id

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
)

// The prefix registry.
//
// Prefixes are declared once per entity type and are immutable thereafter.
// Changing a shipped prefix invalidates every stored identifier that uses it,
// every external alias that resolved to one, and every provenance record
// naming one — so the constraint is worth enforcing rather than remembering.
//
// Each extension declares its prefixes in its own id package and registers
// them from an init function:
//
//	package id
//
//	const (
//	    Substation id.Prefix = "sub"
//	    Bus        id.Prefix = "bus"
//	)
//
//	func init() {
//	    id.MustRegister(Substation, "topology", "Substation")
//	    id.MustRegister(Bus, "topology", "Bus")
//	}
//
// Registration happens at package initialization, so a collision between two
// extensions stops the process at startup with both owners named, rather than
// surfacing as identifiers that parse as the wrong type in production.
//
// The repository-wide inventory test — the one that golden-files the full set
// so that changing a shipped prefix fails CI — lives with the composition root
// that imports every extension, since this package cannot import them without
// inverting the dependency.

// Registration records who owns a prefix and what it names.
type Registration struct {
	// Prefix is the identifier prefix, e.g. "bus".
	Prefix Prefix

	// Owner is the extension that declared it, e.g. "topology".
	Owner string

	// Entity is the human-readable entity name, e.g. "Bus". It appears in
	// collision errors and in the inventory, where a bare prefix is often too
	// terse to recognize.
	Entity string
}

// String renders a registration for error messages and the inventory.
func (r Registration) String() string {
	return fmt.Sprintf("%s (%s.%s)", r.Prefix, r.Owner, r.Entity)
}

//nolint:gochecknoglobals // an init-time registry; the concurrency guard is below
var (
	registryMu sync.RWMutex
	registry   = map[Prefix]Registration{}
)

// Register records a prefix and its owner.
//
// It fails on a malformed prefix, and on any attempt to claim a prefix another
// entity already holds. Re-registering exactly the same prefix, owner and
// entity is allowed and does nothing, so that a package initialized twice —
// which happens under some test layouts — is not an error.
func Register(p Prefix, owner, entity string) error {
	if err := p.Validate(); err != nil {
		return err
	}

	if owner == "" || entity == "" {
		return fmt.Errorf(
			"id: prefix %q must name both an owner and an entity", p)
	}

	incoming := Registration{Prefix: p, Owner: owner, Entity: entity}

	registryMu.Lock()
	defer registryMu.Unlock()

	if existing, taken := registry[p]; taken {
		if existing == incoming {
			return nil
		}

		return fmt.Errorf(
			"id: prefix %q is already registered to %s and cannot be claimed by %s; "+
				"prefixes are immutable once shipped because changing one invalidates "+
				"every stored identifier using it",
			p, existing, incoming)
	}

	registry[p] = incoming

	return nil
}

// MustRegister is Register for use in an init function, where returning an
// error is not possible and continuing with a collision is not acceptable.
func MustRegister(p Prefix, owner, entity string) {
	if err := Register(p, owner, entity); err != nil {
		panic(err)
	}
}

// Lookup returns the registration for a prefix.
func Lookup(p Prefix) (Registration, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()

	r, ok := registry[p]

	return r, ok
}

// Registered returns every registration, ordered by prefix.
//
// The ordering is deterministic so that the inventory can be golden-filed:
// map iteration order would produce a different file on every run.
func Registered() []Registration {
	registryMu.RLock()
	defer registryMu.RUnlock()

	out := make([]Registration, 0, len(registry))
	for _, r := range registry {
		out = append(out, r)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Prefix < out[j].Prefix })

	return out
}

// RegisteredFor returns the registrations owned by one extension, ordered by
// prefix.
func RegisteredFor(owner string) []Registration {
	all := Registered()

	return slices.DeleteFunc(all, func(r Registration) bool {
		return r.Owner != owner
	})
}

// Inventory renders the registry as one line per prefix. This is the form the
// repository-wide golden file takes, so that adding, removing or re-owning a
// prefix shows up as a reviewable diff.
func Inventory() string {
	var b strings.Builder

	for _, r := range Registered() {
		fmt.Fprintf(&b, "%s\t%s\t%s\n", r.Prefix, r.Owner, r.Entity)
	}

	return b.String()
}
