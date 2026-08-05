package asset_test

import (
	"errors"
	"testing"

	"github.com/xraph/blackstart/extensions/geo/asset"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/geo/shape"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// The vocabulary answers two independent questions, and conflating them is the
// mistake a two-valued enum forced. A routed path is real geometry to sample
// weather along; its structures are still invented.
func TestProvenanceSeparatesPathFromStructures(t *testing.T) {
	for _, tc := range []struct {
		p          asset.Provenance
		valid      bool
		pathReal   bool
		structReal bool
	}{
		{asset.Surveyed, true, true, true},
		{asset.Routed, true, true, false},
		{asset.Interpolated, true, false, false},
		{asset.Provenance("guessed"), false, false, false},
	} {
		t.Run(string(tc.p), func(t *testing.T) {
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("Valid() = %v, want %v", got, tc.valid)
			}

			if got := tc.p.PathIsReal(); got != tc.pathReal {
				t.Errorf("PathIsReal() = %v, want %v", got, tc.pathReal)
			}

			if got := tc.p.StructuresAreReal(); got != tc.structReal {
				t.Errorf("StructuresAreReal() = %v, want %v", got, tc.structReal)
			}
		})
	}
}

// Routed must not be mistaken for Surveyed by anything asking whether a
// per-span hazard is conditioned on real structures.
func TestRoutedIsNotSurveyed(t *testing.T) {
	if asset.Routed == asset.Surveyed {
		t.Fatal("Routed and Surveyed must be distinct values")
	}

	if asset.Routed.StructuresAreReal() {
		t.Error("Routed asserts structures that nobody recorded")
	}
}

// A route carries a branch or a circuit and nothing else. The prefix is the
// only discriminator, so an unrecognized one has to be refused at the door:
// stored, it would read back as a complete route whose carrier nothing can
// resolve — the invisible failure the real-circuit-identity design, §2, rejects
// manufacturing branches to avoid.
func TestRouteRefusesACarrierNothingCanResolve(t *testing.T) {
	for _, tc := range []struct {
		name    string
		carrier id.ID
		ok      bool
	}{
		{"branch in a network model", id.MustNew("nmdl"), true},
		{"circuit somebody built", id.MustNew("circ"), true},
		{"a substation is not a carrier", id.MustNew("sub"), false},
		{"a span is not a carrier", id.MustNew("span"), false},
		{"nothing at all", id.ID{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			route := asset.Route{
				ID:         gid.NewRoute(),
				CarrierID:  tc.carrier,
				Provenance: asset.Routed,
				LengthM:    1000,
			}

			err := route.Validate()
			if tc.ok && err != nil {
				t.Fatalf("Validate() = %v, want a stored route", err)
			}

			if !tc.ok {
				if err == nil {
					t.Fatal("Validate() = nil, want a refusal")
				}

				if !errors.Is(err, bserr.ErrInvalid) {
					t.Errorf("Validate() = %v, want ErrInvalid", err)
				}
			}
		})
	}
}

// Only a branch can be solved. A circuit carries no impedance by design, and
// anything electrical asks here rather than discovering it inside a solver.
func TestOnlyABranchCarriesElectrically(t *testing.T) {
	if !asset.CarriesElectrically(id.MustNew("nmdl")) {
		t.Error("a branch in a network model should carry electrically")
	}

	if asset.CarriesElectrically(id.MustNew("circ")) {
		t.Error("a circuit has no electrical parameters and must not be solvable")
	}
}

func TestAStructureNeedsARouteAndAPlaceOnIt(t *testing.T) {
	loose := asset.Structure{
		ID:       gid.NewStructure(),
		Location: shape.Point{Lon: -97.8, Lat: 30.3},
	}

	if err := loose.Validate(); err == nil {
		t.Fatal("a structure on no route validated; a tower that stands on " +
			"nothing is not part of any line")
	}
}

func TestAStructureKeepsWhatTheSourceSaid(t *testing.T) {
	structure := asset.Structure{
		ID:       gid.NewStructure(),
		RouteID:  gid.NewRoute(),
		Ordinal:  0,
		Location: shape.Point{Lon: -97.8, Lat: 30.3},
		Kind:     "tower",
		Material: "steel",
	}

	if err := structure.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestAnUnclassedStructureIsStillValid(t *testing.T) {
	// 2.2% of OSM line-way vertices carry no class. They are still positions
	// the conductor passes over, and refusing them would drop real geometry to
	// avoid an empty string.
	structure := asset.Structure{
		ID:       gid.NewStructure(),
		RouteID:  gid.NewRoute(),
		Location: shape.Point{Lon: -97.8, Lat: 30.3},
	}

	if err := structure.Validate(); err != nil {
		t.Fatalf("an unclassed structure was refused: %v", err)
	}
}
