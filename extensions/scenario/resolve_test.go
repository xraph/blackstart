package scenario

import (
	"math"
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/networkmodel/canonical"
	nmid "github.com/xraph/blackstart/extensions/networkmodel/id"
	nmmodel "github.com/xraph/blackstart/extensions/networkmodel/model"
	scn "github.com/xraph/blackstart/extensions/scenario/scenario"
)

// theModelName is what the stored row calls the network. It is asserted
// rather than ignored because it reaches the wire model the solver hashes.
const theModelName = "texas synthetic"

// aCanonicalModel is a five-branch network whose branch identifiers are the
// same platform ones the geometry fakes answer for, so a model and an asset
// record can actually be joined here.
func aCanonicalModel() canonical.Model {
	buses := []canonical.Bus{
		{ID: "bus_a", Kind: 3, VMagnitude: 1, QMin: math.Inf(-1), QMax: math.Inf(1)},
		{
			ID: "bus_b", Kind: 1, VMagnitude: 1, PInjection: -0.5,
			QMin: math.Inf(-1), QMax: math.Inf(1),
		},
	}

	branches := make([]canonical.Branch, 0, len(lines))
	for _, line := range lines {
		branches = append(branches, canonical.Branch{
			ID: line.String(), FromID: "bus_a", ToID: "bus_b",
			R: 0.01, X: 0.1, Rating: 1.5, InService: true, Tap: 1,
		})
	}

	return canonical.Model{SBaseVA: 100e6, VBaseV: 345_000, Buses: buses, Branches: branches}
}

// storedModel is a network as the model reader hands it over: the row, its
// canonical bytes, and the content hash that names both.
func storedModel(t *testing.T) (nmmodel.NetworkModel, []byte, string) {
	t.Helper()

	model := aCanonicalModel()

	encoded, err := canonical.Encode(model)
	if err != nil {
		t.Fatalf("encoding the fixture: %v", err)
	}

	hash, err := canonical.Hash(model)
	if err != nil {
		t.Fatalf("hashing the fixture: %v", err)
	}

	return nmmodel.NetworkModel{
		ID:          nmid.NewModel(),
		ContentHash: hash,
		Name:        theModelName,
		BusCount:    len(model.Buses),
		BranchCount: len(model.Branches),
	}, encoded, hash
}

// executable wires the model fakes to a real stored network and returns the
// manifest that pins it.
func executable(t *testing.T, f *fakes) scn.Manifest {
	t.Helper()

	found, encoded, hash := storedModel(t)
	f.models.found, f.models.encoded = found, encoded

	manifest := validManifest()
	manifest.ModelHash = hash

	return manifest
}

// Resolving turns the manifest's two pinned names into the things they name:
// the network in the solver's contract, under its stored name, and the
// cycle's issuance — which is the knowledge-time axis every rating lookup
// this run makes is keyed on.
func TestResolveReturnsTheModelAndTheCyclesIssuance(t *testing.T) {
	t.Parallel()

	f := newFakes()
	e, _ := engineWith(t, f)
	manifest := executable(t, f)

	got, err := e.resolve(t.Context(), manifest)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if got.model.GetName() != theModelName {
		t.Errorf("model resolved under the name %q, want the stored %q; the name "+
			"reaches the hash the solver keys its cache on",
			got.model.GetName(), theModelName)
	}

	if len(got.model.GetBranches()) != len(lines) {
		t.Errorf("resolved %d branches, want %d", len(got.model.GetBranches()), len(lines))
	}

	if !got.issuedAt.Equal(theCycle.IssuedAt) {
		t.Errorf("issued at %v, want the cycle's %v", got.issuedAt, theCycle.IssuedAt)
	}
}

// A model hash that no longer resolves fails naming the model, because the
// hash is the only thing a reader has to go and look for.
func TestAModelThatCannotBeResolvedFailsNamingIt(t *testing.T) {
	t.Parallel()

	f := newFakes()
	e, _ := engineWith(t, f)
	manifest := executable(t, f)
	f.models.foundErr = errNotFound

	_, err := e.resolve(t.Context(), manifest)
	if err == nil {
		t.Fatal("want an error: the pinned model does not resolve")
	}

	if !strings.Contains(err.Error(), manifest.ModelHash) {
		t.Errorf("the error does not name the model: %v", err)
	}
}

// The manifest pins a model by hash AND verifies it. A model re-imported or
// repaired under the same name leaves the row pointing at different bytes,
// and a run against those is not the run the manifest describes.
//
// The message names both hashes: what was pinned and what is there now.
func TestADriftedModelFailsNamingWhatDrifted(t *testing.T) {
	t.Parallel()

	f := newFakes()
	e, _ := engineWith(t, f)
	manifest := executable(t, f)

	// The same network with one impedance changed: a different network, and
	// the encoding hashes accordingly.
	drifted := aCanonicalModel()
	drifted.Branches[0].X = 0.2

	encoded, err := canonical.Encode(drifted)
	if err != nil {
		t.Fatalf("encoding the drifted model: %v", err)
	}

	driftedHash, err := canonical.Hash(drifted)
	if err != nil {
		t.Fatalf("hashing the drifted model: %v", err)
	}

	f.models.encoded = encoded

	_, err = e.resolve(t.Context(), manifest)
	if err == nil {
		t.Fatal("want an error: the stored encoding is not the pinned model")
	}

	for _, want := range []string{manifest.ModelHash, driftedHash, theModelName} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %q: %v", want, err)
		}
	}
}

// A cycle the manifest names but the platform cannot produce fails naming the
// cycle — both when the string is not a cycle identifier at all and when it
// is one nothing has recorded.
func TestACycleThatCannotBeResolvedFailsNamingIt(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		cycle string
		err   error
	}{
		{"not an identifier", "gfs-2026080400", nil},
		{"never recorded", theCycle.ID.String(), errNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFakes()
			e, _ := engineWith(t, f)
			manifest := executable(t, f)
			manifest.WeatherCycle = tc.cycle
			f.cycles.err = tc.err

			_, err := e.resolve(t.Context(), manifest)
			if err == nil {
				t.Fatal("want an error: the pinned cycle does not resolve")
			}

			if !strings.Contains(err.Error(), tc.cycle) {
				t.Errorf("the error does not name the cycle: %v", err)
			}
		})
	}
}

// A run that rated nothing gets one of two errors, and they are different
// errors because they need different fixes. Sending a reader to look at the
// weather when their model's branch identifiers do not exist in the asset
// record wastes the one piece of information the failure had to offer.
func TestRatingGapTellsTheTwoZeroRatedCasesApart(t *testing.T) {
	t.Parallel()

	manifest := validManifest()

	unjoined := ratingGap(manifest, Rated{BranchCount: 5, Located: 0})
	if unjoined == nil {
		t.Fatal("want an error: five branches, none of them joined, nothing rated")
	}

	unrated := ratingGap(manifest, Rated{BranchCount: 5, Located: 5})
	if unrated == nil {
		t.Fatal("want an error: five branches located and the cycle rated none")
	}

	if unjoined.Error() == unrated.Error() {
		t.Fatalf("both zero-rated causes report the same thing: %v", unjoined)
	}

	// The join failure must not blame the forecast, and the forecast failure
	// must name the cycle and the instant — the pair that decides which
	// numbers were looked for.
	if !strings.Contains(unjoined.Error(), "asset record") {
		t.Errorf("the unjoined error does not say where to look: %v", unjoined)
	}

	if !strings.Contains(unrated.Error(), manifest.WeatherCycle) ||
		!strings.Contains(unrated.Error(), "2026-08-03T18:00:00Z") {
		t.Errorf("the unrated error names neither the cycle nor the instant: %v", unrated)
	}
}

// Some coverage is not a failure. A network with rated branches runs, however
// few there are, because the count is on the record for a reader to judge.
func TestRatingGapPassesWhateverWasRated(t *testing.T) {
	t.Parallel()

	manifest := validManifest()

	if err := ratingGap(manifest, Rated{BranchCount: 2000, Located: 3, WeatherRated: 1}); err != nil {
		t.Errorf("one rated branch of two thousand is a finding, not a failure: %v", err)
	}

	// A model with no branches is not a weather problem, and the screen
	// refuses it a moment later naming the actual reason.
	if err := ratingGap(manifest, Rated{}); err != nil {
		t.Errorf("an empty model failed at the rating step: %v", err)
	}
}
