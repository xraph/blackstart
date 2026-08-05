package fragility_test

import (
	"testing"

	"github.com/xraph/blackstart/extensions/risk/fragility"
)

func TestOSMKindsMapToClasses(t *testing.T) {
	for kind, want := range map[string]fragility.Class{
		"tower": fragility.Lattice,
		"pole":  fragility.Pole,
	} {
		got, ok := fragility.ClassOf(kind)
		if !ok || got != want {
			t.Errorf("ClassOf(%q) = %v, %v; want %v, true", kind, got, ok, want)
		}
	}
}

func TestAnUnclassedStructureIsReportedNotGuessed(t *testing.T) {
	// The survey measured 2.2% of line-way vertices carrying no class, and an
	// ArcGIS-sourced route has none at all. Defaulting them to a lattice tower
	// would put a real curve on a structure nobody classified, and the result
	// would be indistinguishable from a measured one.
	//
	// "portal" belongs in this set for a different reason than "monopole" and
	// "H-frame": those two lack a class because no fragility curve exists for
	// them (see the [fragility.Class] doc). "portal" lacks a class because
	// fragility.Portal itself does not exist — the previous task searched the
	// open-access and national-laboratory literature and found no citable
	// curve for portal structures, so the type was never added to the closed
	// set. OSM's power=portal tag is not rare: the phase-3 survey measured it
	// at roughly 4% of line-way nodes, so this is a known and quantified gap,
	// not an oversight.
	for _, kind := range []string{"", "monopole", "H-frame", "portal", "unknown"} {
		if _, ok := fragility.ClassOf(kind); ok {
			t.Errorf("ClassOf(%q) claimed a class; unrecognized kinds must be "+
				"reported, not coerced", kind)
		}
	}
}
