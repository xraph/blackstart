package id

import (
	"strings"
	"sync"
	"testing"
)

// These tests share the package-level registry, so each uses prefixes no other
// test claims. That constraint mirrors production: the registry is global by
// design, because the collisions it exists to catch are global.

func TestRegisterAndLookup(t *testing.T) {
	if err := Register("rgbus", "topology", "Bus"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, ok := Lookup("rgbus")
	if !ok {
		t.Fatal("registered prefix not found")
	}

	if got.Owner != "topology" || got.Entity != "Bus" {
		t.Errorf("Lookup = %+v, want owner topology entity Bus", got)
	}

	if _, ok := Lookup("rgnothing"); ok {
		t.Error("an unregistered prefix was found")
	}
}

// The central point of the registry. Two extensions claiming the same prefix
// is a bug that would otherwise surface as identifiers parsing as the wrong
// type, long after the cause.
func TestRegisterRejectsACollision(t *testing.T) {
	if err := Register("rgdup", "topology", "Bus"); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	err := Register("rgdup", "weather", "Station")
	if err == nil {
		t.Fatal("a second extension claimed a prefix already in use")
	}

	// The message must name both sides, or the reader has to grep for the
	// other owner to understand the conflict.
	for _, want := range []string{"topology", "weather", "rgdup"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("collision error should mention %q, got %q", want, err)
		}
	}
}

// A package can be initialized more than once under some test layouts, and an
// identical re-registration is not a conflict.
func TestRegisterIsIdempotentForIdenticalEntries(t *testing.T) {
	if err := Register("rgidem", "topology", "Branch"); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	if err := Register("rgidem", "topology", "Branch"); err != nil {
		t.Errorf("identical re-registration should be accepted: %v", err)
	}

	// But re-registering with a different entity is still a collision: it
	// means two entities in one extension share a prefix.
	if err := Register("rgidem", "topology", "Transformer"); err == nil {
		t.Error("two entities in one extension claimed the same prefix")
	}
}

func TestRegisterRejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name          string
		prefix        Prefix
		owner, entity string
	}{
		{"malformed prefix", "RGBad", "topology", "Bus"},
		{"empty prefix", "", "topology", "Bus"},
		{"no owner", "rgnoowner", "", "Bus"},
		{"no entity", "rgnoentity", "topology", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Register(tt.prefix, tt.owner, tt.entity); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestMustRegisterPanicsOnCollision(t *testing.T) {
	MustRegister("rgpanic", "topology", "Bus")

	defer func() {
		if recover() == nil {
			t.Error("MustRegister did not panic on a collision; an init-time " +
				"collision must stop the process, not be ignored")
		}
	}()

	MustRegister("rgpanic", "weather", "Station")
}

// The inventory is golden-filed by the composition root, so its ordering must
// not depend on map iteration.
func TestRegisteredIsOrderedByPrefix(t *testing.T) {
	MustRegister("rgzz", "z", "Z")
	MustRegister("rgaa", "a", "A")
	MustRegister("rgmm", "m", "M")

	all := Registered()

	for i := 1; i < len(all); i++ {
		if all[i-1].Prefix >= all[i].Prefix {
			t.Fatalf("registrations are not ordered: %s before %s",
				all[i-1].Prefix, all[i].Prefix)
		}
	}
}

func TestInventoryIsStableAndTabSeparated(t *testing.T) {
	MustRegister("rginv", "topology", "Inventory")

	first := Inventory()
	if first != Inventory() {
		t.Fatal("Inventory is not stable across calls; it cannot be golden-filed")
	}

	var found bool

	for line := range strings.SplitSeq(first, "\n") {
		if !strings.HasPrefix(line, "rginv\t") {
			continue
		}

		found = true

		if got := strings.Split(line, "\t"); len(got) != 3 {
			t.Errorf("inventory line %q has %d fields, want 3", line, len(got))
		}
	}

	if !found {
		t.Error("a registered prefix is missing from the inventory")
	}
}

func TestRegisteredFor(t *testing.T) {
	MustRegister("rgowna", "ownertest", "A")
	MustRegister("rgownb", "ownertest", "B")
	MustRegister("rgownc", "othertest", "C")

	got := RegisteredFor("ownertest")
	if len(got) != 2 {
		t.Fatalf("RegisteredFor returned %d registrations, want 2: %v", len(got), got)
	}

	for _, r := range got {
		if r.Owner != "ownertest" {
			t.Errorf("RegisteredFor returned a registration owned by %q", r.Owner)
		}
	}
}

// Registration happens from init functions across many packages, which the Go
// runtime serializes — but Lookup and Registered are called from request
// handlers. The mutex is checked under -race.
func TestConcurrentAccessIsSafe(t *testing.T) {
	MustRegister("rgrace", "topology", "Race")

	var wg sync.WaitGroup

	for i := range 50 {
		wg.Add(2)

		go func() {
			defer wg.Done()

			_, _ = Lookup("rgrace")
			_ = Registered()
		}()

		go func() {
			defer wg.Done()

			// Distinct prefixes, so these are additions rather than conflicts.
			_ = Register(Prefix("rgconc"+strings.Repeat("a", i%20+1)), "topology", "Concurrent")
		}()
	}

	wg.Wait()
}
