package canonical_test

import (
	"math"
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/networkmodel/canonical"
)

func model() canonical.Model {
	return canonical.Model{
		SBaseVA: 100e6,
		VBaseV:  138e3,
		Buses: []canonical.Bus{
			{ID: "bus-a", Kind: 3, VMagnitude: 1.0},
			{ID: "bus-b", Kind: 1, PInjection: -0.5, QInjection: -0.1, VMagnitude: 1.0},
		},
		Branches: []canonical.Branch{
			{ID: "line-1", FromID: "bus-a", ToID: "bus-b", R: 0.01, X: 0.1, Tap: 1},
		},
	}
}

// The property everything downstream depends on: one network, one hash. The
// solver cache is keyed on it, a provenance record names it, and a result
// carries it.
func TestTheSameModelAlwaysHashesTheSame(t *testing.T) {
	first, err := canonical.Hash(model())
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if len(first) != 64 {
		t.Errorf("hash %q is not a hex sha256", first)
	}

	for range 50 {
		again, err := canonical.Hash(model())
		if err != nil {
			t.Fatalf("Hash: %v", err)
		}

		if again != first {
			t.Fatalf("one model hashed to %q then %q", first, again)
		}
	}
}

// The caller's ordering must not leak into the hash. Once models come from a
// database, that order is whatever the query returned — and a hash that moved
// with it would miss the cache on every read of the same network.
func TestTheCallersOrderingDoesNotChangeTheHash(t *testing.T) {
	forward := model()

	reversed := model()
	reversed.Buses[0], reversed.Buses[1] = reversed.Buses[1], reversed.Buses[0]

	a, err := canonical.Hash(forward)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	b, err := canonical.Hash(reversed)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if a != b {
		t.Errorf("reordering the bus list changed the hash: %s vs %s", a, b)
	}
}

// Branches reference buses by identifier precisely so that reordering cannot
// repoint them. This checks the resolution actually followed the sort rather
// than happening to line up.
func TestBranchesFollowTheirBusesThroughTheSort(t *testing.T) {
	forward := model()

	reversed := model()
	reversed.Buses[0], reversed.Buses[1] = reversed.Buses[1], reversed.Buses[0]

	a, err := canonical.Encode(forward)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	b, err := canonical.Encode(reversed)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	if string(a) != string(b) {
		t.Error("the encoded bytes differ after reordering, so a branch's bus " +
			"indices did not follow the sort")
	}
}

// Physics changes the hash. Every one of these describes a different network,
// and a cache that returned the same answer for any pair would be returning
// the wrong one.
func TestEveryPhysicalChangeChangesTheHash(t *testing.T) {
	base, err := canonical.Hash(model())
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	cases := map[string]func(*canonical.Model){
		"reactance":      func(m *canonical.Model) { m.Branches[0].X = 0.2 },
		"resistance":     func(m *canonical.Model) { m.Branches[0].R = 0.02 },
		"charging":       func(m *canonical.Model) { m.Branches[0].B = 0.05 },
		"rating":         func(m *canonical.Model) { m.Branches[0].Rating = 2 },
		"tap":            func(m *canonical.Model) { m.Branches[0].Tap = 1.05 },
		"phase shift":    func(m *canonical.Model) { m.Branches[0].Shift = 0.1 },
		"in service":     func(m *canonical.Model) { m.Branches[0].InService = true },
		"injection":      func(m *canonical.Model) { m.Buses[1].PInjection = -0.6 },
		"reactive":       func(m *canonical.Model) { m.Buses[1].QInjection = -0.2 },
		"bus kind":       func(m *canonical.Model) { m.Buses[1].Kind = 2 },
		"voltage":        func(m *canonical.Model) { m.Buses[1].VMagnitude = 1.02 },
		"angle":          func(m *canonical.Model) { m.Buses[0].VAngle = 0.5 },
		"shunt":          func(m *canonical.Model) { m.Buses[1].BShunt = 0.3 },
		"reactive limit": func(m *canonical.Model) { m.Buses[0].QMax = 1 },
		// The bus's own nominal voltage is physics, not presentation: two
		// networks identical in per unit and at different voltage levels
		// carry different currents for the same flow, and Import
		// deduplicates on this hash. A hash that ignored it would hand back
		// the first network's voltages for the second network's lines.
		"bus nominal voltage": func(m *canonical.Model) { m.Buses[1].VNominalV = 138e3 },
		"apparent base":       func(m *canonical.Model) { m.SBaseVA = 200e6 },
		"voltage base":        func(m *canonical.Model) { m.VBaseV = 230e3 },
		"an extra bus":        func(m *canonical.Model) { m.Buses = append(m.Buses, canonical.Bus{ID: "bus-c"}) },
		"an extra branch": func(m *canonical.Model) {
			m.Branches = append(m.Branches, canonical.Branch{ID: "l2", FromID: "bus-a", ToID: "bus-b", Tap: 1})
		},
	}

	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			m := model()
			change(&m)

			got, err := canonical.Hash(m)
			if err != nil {
				t.Fatalf("Hash: %v", err)
			}

			if got == base {
				t.Errorf("changing the %s did not change the hash", name)
			}
		})
	}
}

// The base is hashed because the same numbers on a different base describe a
// different network. A cache keyed on a hash that ignored it would return one
// network's answer for another's question — checked explicitly above, and
// stated here because it is the least obvious of the lot.
func TestTheBaseIsPartOfTheIdentity(t *testing.T) {
	a := model()
	b := model()
	b.SBaseVA = 50e6

	first, _ := canonical.Hash(a)
	second, _ := canonical.Hash(b)

	if first == second {
		t.Error("two different per-unit bases produced one hash")
	}
}

// A branch naming a bus that is not in the model would encode a meaningless
// index. Refused with both names, because "invalid model" does not say which
// branch or which bus.
func TestADanglingBranchIsRefusedWithBothNames(t *testing.T) {
	m := model()
	m.Branches[0].ToID = "bus-missing"

	_, err := canonical.Encode(m)
	if err == nil {
		t.Fatal("Encode accepted a branch naming a bus that is not in the model")
	}

	if !strings.Contains(err.Error(), "line-1") || !strings.Contains(err.Error(), "bus-missing") {
		t.Errorf("message %q does not name both the branch and the bus", err)
	}
}

func TestADuplicateBusIsRefused(t *testing.T) {
	m := model()
	m.Buses = append(m.Buses, canonical.Bus{ID: "bus-a"})

	if _, err := canonical.Encode(m); err == nil {
		t.Fatal("Encode accepted two buses with one identifier")
	}
}

// IEEE-754 permits many NaN bit patterns, and two models identical in every
// respect except which one they carry would otherwise hash differently — a
// distinction with no physical meaning.
func TestNaNIsNormalized(t *testing.T) {
	a := model()
	a.Buses[0].QMax = math.NaN()

	b := model()
	b.Buses[0].QMax = math.Float64frombits(math.Float64bits(math.NaN()) | 0x5)

	if !math.IsNaN(b.Buses[0].QMax) {
		t.Fatal("the second value is not a NaN; this test proves nothing")
	}

	first, _ := canonical.Hash(a)
	second, _ := canonical.Hash(b)

	if first != second {
		t.Error("two NaN bit patterns produced two hashes")
	}
}

// Negative zero and positive zero are equal in every comparison the solver
// makes and describe the same network.
func TestNegativeZeroHashesAsZero(t *testing.T) {
	a := model()
	a.Buses[1].QInjection = 0

	b := model()
	b.Buses[1].QInjection = math.Copysign(0, -1)

	first, _ := canonical.Hash(a)
	second, _ := canonical.Hash(b)

	if first != second {
		t.Error("negative zero and positive zero produced two hashes")
	}
}

// The version is the first byte, so an old hash and a new one can never
// collide by accident and a stored hash says which encoder produced it.
func TestTheVersionLeadsTheEncoding(t *testing.T) {
	encoded, err := canonical.Encode(model())
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	if len(encoded) == 0 || encoded[0] != canonical.Version {
		t.Errorf("the encoding does not begin with version %d", canonical.Version)
	}
}

// A name is not physics. Renaming a substation must not invalidate every
// cached solve of the network it sits in — which is why the encoder lists its
// fields explicitly rather than reflecting over the struct.
//
// There is no name field to change here, and that absence is the point: this
// test documents the boundary so that adding one to Bus is a decision someone
// has to make rather than something that happens.
func TestOnlyPhysicsIsEncoded(t *testing.T) {
	encoded, err := canonical.Encode(model())
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// The identifier is present — it fixes ordering — but nothing that reads
	// like prose should be.
	for _, prose := range []string{"Substation", "230 kV", "description"} {
		if strings.Contains(string(encoded), prose) {
			t.Errorf("the encoding contains %q, which is not physics", prose)
		}
	}
}

// One definition of "states a voltage", because there were three and +Inf
// passed two of them. A builder computing a nominal voltage by division
// produces +Inf, and a model carrying one used to hash, store, cross the wire
// and reach the viewer — where encoding/json refuses to marshal an infinity
// and the endpoint fails with an error naming JSON rather than voltage.
func TestOnlyAUsableVoltageCountsAsStated(t *testing.T) {
	cases := map[string]struct {
		volts  float64
		stated bool
	}{
		"a real voltage": {138e3, true},
		"the smallest":   {math.SmallestNonzeroFloat64, true},
		"unstated":       {0, false},
		"negative":       {-138e3, false},
		"negative zero":  {math.Copysign(0, -1), false},
		"positive infin": {math.Inf(1), false},
		"negative infin": {math.Inf(-1), false},
		"not a number":   {math.NaN(), false},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			volts, stated := canonical.Bus{ID: "b", VNominalV: c.volts}.StatedNominalV()

			if stated != c.stated {
				t.Errorf("stated = %v, want %v for %v V", stated, c.stated, c.volts)
			}

			// An unstated bus reports zero rather than the unusable value, so
			// a caller that ignores the second return cannot convert against
			// an infinity by accident.
			if !stated && volts != 0 {
				t.Errorf("an unstated bus reported %v V, want 0", volts)
			}

			if stated && volts != c.volts {
				t.Errorf("a stated bus reported %v V, want %v", volts, c.volts)
			}
		})
	}
}

// A nominal voltage no bus could have is refused rather than hashed.
//
// Every consumer reads −138 kV, NaN and +Inf the same way it reads zero: the
// bus states nothing. So a model carrying one behaves identically to the model
// with a zero there and hashes differently — two networks nobody can tell
// apart, stored as two rows with two content hashes. That is the deduplication
// collision this field was hashed to prevent, running backwards.
func TestAnImpossibleNominalVoltageIsRefused(t *testing.T) {
	for name, volts := range map[string]float64{
		"negative":          -138e3,
		"positive infinity": math.Inf(1),
		"negative infinity": math.Inf(-1),
		"not a number":      math.NaN(),
	} {
		t.Run(name, func(t *testing.T) {
			m := model()
			m.Buses[1].VNominalV = volts

			_, err := canonical.Encode(m)
			if err == nil {
				t.Fatalf("Encode accepted a bus at %v V", volts)
			}

			// Named, because a model has two thousand buses and "invalid
			// voltage" sends a reader to look through all of them.
			if !strings.Contains(err.Error(), "bus-b") {
				t.Errorf("message %q does not name the bus", err)
			}
		})
	}
}

// Zero stays legal, and it has to: it is how a bus says it states none, and
// every model built before the field existed has it on every bus.
func TestAnUnstatedNominalVoltageStillEncodes(t *testing.T) {
	m := model()

	for _, bus := range m.Buses {
		if bus.VNominalV != 0 {
			t.Fatalf("the fixture states a voltage for %s", bus.ID)
		}
	}

	if _, err := canonical.Encode(m); err != nil {
		t.Errorf("Encode refused a model whose buses state no voltage: %v", err)
	}
}
