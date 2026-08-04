package asset_test

import (
	"testing"

	"github.com/xraph/blackstart/extensions/geo/asset"
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
