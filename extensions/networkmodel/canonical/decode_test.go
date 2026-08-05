package canonical_test

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/networkmodel/canonical"
)

func threeBusModel() canonical.Model {
	return canonical.Model{
		SBaseVA: 100e6,
		VBaseV:  138e3,
		// Deliberately out of sorted order, so a decode that preserved input
		// order rather than encoded order would produce different bytes.
		Buses: []canonical.Bus{
			// Two voltage levels and one bus that states none, so the round
			// trip covers a value that varies per bus rather than one
			// repeated three times — which a decoder reading the model base
			// into every bus would reproduce exactly.
			{
				ID: "bus-3", Kind: 1, PInjection: -0.942, QInjection: -0.19,
				VMagnitude: 1.01, VAngle: -0.222, BShunt: 0.19,
				QMin: math.Inf(-1), QMax: math.Inf(1), VNominalV: 138e3,
			},
			{
				ID: "bus-1", Kind: 3, VMagnitude: 1.06,
				QMin: math.Inf(-1), QMax: math.Inf(1), VNominalV: 345e3,
			},
			{
				ID: "bus-2", Kind: 2, PInjection: 0.183, QInjection: -0.127,
				VMagnitude: 1.045, VAngle: -0.0869, QMin: -0.4, QMax: 0.5,
			},
		},
		Branches: []canonical.Branch{
			{
				ID: "branch-1", FromID: "bus-2", ToID: "bus-3",
				R: 0.04699, X: 0.19797, B: 0.0438, Rating: 1.5,
				InService: true, Tap: 1,
			},
			{
				ID: "branch-0", FromID: "bus-1", ToID: "bus-2",
				R: 0.01938, X: 0.05917, B: 0.0528, Rating: 2,
				InService: true, Tap: 1,
			},
			{
				ID: "branch-2", FromID: "bus-1", ToID: "bus-3",
				R: 0.05403, X: 0.22304, B: 0.0492, Rating: 1.75,
				InService: false, Tap: 0.98, Shift: 0.05236,
			},
		},
	}
}

// The property that matters: bytes in, bytes out. The stored blob is the only
// copy of a network once its source file has moved on, and a lossy decode
// would mean a re-export produced a different content hash for a network that
// had not changed.
func TestDecodeReproducesTheEncoding(t *testing.T) {
	encoded, err := canonical.Encode(threeBusModel())
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := canonical.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	again, err := canonical.Encode(decoded)
	if err != nil {
		t.Fatalf("re-Encode: %v", err)
	}

	if !bytes.Equal(encoded, again) {
		t.Errorf("re-encoding a decoded model produced %d bytes, not the original %d",
			len(again), len(encoded))
	}

	// And therefore the same hash, which is what everything else names a model
	// by.
	first, _ := canonical.Hash(threeBusModel())
	second, _ := canonical.Hash(decoded)

	if first != second {
		t.Errorf("hash changed across a round trip: %s then %s", first, second)
	}
}

// Branch endpoints come back as identifiers, and they must be the same
// endpoints. An off-by-one in the index would still decode and would describe
// a different network.
func TestBranchEndpointsSurviveTheRoundTrip(t *testing.T) {
	encoded, _ := canonical.Encode(threeBusModel())

	decoded, err := canonical.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	want := map[string][2]string{
		"branch-0": {"bus-1", "bus-2"},
		"branch-1": {"bus-2", "bus-3"},
		"branch-2": {"bus-1", "bus-3"},
	}

	for _, branch := range decoded.Branches {
		ends, ok := want[branch.ID]
		if !ok {
			t.Fatalf("unexpected branch %q", branch.ID)
		}

		if branch.FromID != ends[0] || branch.ToID != ends[1] {
			t.Errorf("branch %s runs %s→%s, want %s→%s",
				branch.ID, branch.FromID, branch.ToID, ends[0], ends[1])
		}
	}

	// Out-of-service survives too. A branch that came back in service would
	// change every flow in the network.
	for _, branch := range decoded.Branches {
		if branch.ID == "branch-2" && branch.InService {
			t.Error("an out-of-service branch decoded as in service")
		}
	}
}

// Infinities are how "no reactive limit" is spelled, and they must not decode
// as a large finite number — which would pin a generator that has no limit.
func TestUnlimitedReactivePowerStaysUnlimited(t *testing.T) {
	encoded, _ := canonical.Encode(threeBusModel())

	decoded, err := canonical.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	for _, bus := range decoded.Buses {
		if bus.ID != "bus-1" {
			continue
		}

		if !math.IsInf(bus.QMin, -1) || !math.IsInf(bus.QMax, 1) {
			t.Errorf("bus-1 came back with limits %v to %v", bus.QMin, bus.QMax)
		}
	}
}

// The voltage a bus sits at is SI volts where everything around it is per
// unit, and it varies from bus to bus. A decode that read it out of the wrong
// slot, or that filled it from the model base, would produce a network whose
// lines are rated against a voltage nobody stated for them.
func TestEachBusesNominalVoltageSurvivesTheRoundTrip(t *testing.T) {
	encoded, _ := canonical.Encode(threeBusModel())

	decoded, err := canonical.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	want := map[string]float64{"bus-1": 345e3, "bus-2": 0, "bus-3": 138e3}

	for _, bus := range decoded.Buses {
		if got := bus.VNominalV; got != want[bus.ID] {
			t.Errorf("%s came back at %v V, want %v", bus.ID, got, want[bus.ID])
		}
	}
}

// A blob written by the version-1 encoder has no per-bus voltage, so every
// byte after the first bus is at a different offset. It must be refused
// rather than misread: reading one anyway would take a nominal voltage out of
// the next bus's identifier and produce a network that decodes cleanly and
// describes something else. Re-importing the source is the remedy.
func TestAVersionOneEncodingIsRefused(t *testing.T) {
	encoded, _ := canonical.Encode(threeBusModel())

	stale := bytes.Clone(encoded)
	stale[0] = 1

	_, err := canonical.Decode(stale)
	if err == nil {
		t.Fatal("a version-1 encoding decoded cleanly")
	}

	if errors.Is(err, canonical.ErrTruncated) {
		t.Errorf("a version mismatch was reported as truncation: %v", err)
	}
}

// Corrupt bytes must be reported rather than producing a plausible network. A
// decoder that guessed would turn a truncated file into a network with a
// missing line, which solves and is wrong.
func TestCorruptEncodingsAreRefused(t *testing.T) {
	encoded, _ := canonical.Encode(threeBusModel())

	t.Run("truncated at every length", func(t *testing.T) {
		for cut := 1; cut < len(encoded); cut++ {
			if _, err := canonical.Decode(encoded[:cut]); err == nil {
				t.Fatalf("a %d-byte prefix of a %d-byte encoding decoded cleanly",
					cut, len(encoded))
			}
		}
	})

	t.Run("trailing bytes", func(t *testing.T) {
		if _, err := canonical.Decode(append(bytes.Clone(encoded), 0)); err == nil {
			t.Error("an encoding with a trailing byte decoded cleanly")
		}
	})

	t.Run("wrong version", func(t *testing.T) {
		wrong := bytes.Clone(encoded)
		wrong[0] = canonical.Version + 1

		_, err := canonical.Decode(wrong)
		if err == nil {
			t.Fatal("a future version decoded cleanly")
		}

		if errors.Is(err, canonical.ErrTruncated) {
			t.Errorf("a version mismatch was reported as truncation: %v", err)
		}
	})

	t.Run("empty", func(t *testing.T) {
		if _, err := canonical.Decode(nil); !errors.Is(err, canonical.ErrTruncated) {
			t.Errorf("got %v, want ErrTruncated", err)
		}
	})
}

// The error an operator actually meets, through `bsctl model show` or a
// scenario run that would not resolve. Naming both versions says what
// happened; it does not say what to do, and "re-import" lived only in a
// comment beside the version constant where nobody hitting the error will
// read it. The new hash is stated too, so it is not discovered later by a
// manifest that keeps failing.
func TestTheVersionRefusalNamesTheRemedy(t *testing.T) {
	encoded, _ := canonical.Encode(threeBusModel())

	stale := bytes.Clone(encoded)
	stale[0] = 1

	_, err := canonical.Decode(stale)
	if err == nil {
		t.Fatal("a version-1 encoding decoded cleanly")
	}

	for _, want := range []string{"Re-import", "content hash"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not mention %q", err, want)
		}
	}
}
