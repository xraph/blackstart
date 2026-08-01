package quantity

import (
	"encoding/json"
	"testing"
)

// The three keys are a public contract: they appear in every user-authored
// formula. This test exists so that renaming one fails here rather than in
// somebody's saved work.
func TestDTLKeysAreFrozen(t *testing.T) {
	t.Parallel()

	m := Megawatts(1).ToDTL()

	for _, key := range []string{"magnitude", "unit", "base"} {
		if _, ok := m[key]; !ok {
			t.Errorf("the DTL projection must contain %q; renaming a key breaks "+
				"every formula that reads it", key)
		}
	}

	if len(m) != 3 {
		t.Errorf("the DTL projection has %d keys, want exactly 3", len(m))
	}

	// DTL's value domain has no float32, decimal, or big.Float. Anything it
	// does not recognize is flattened to zero without an error, so the
	// magnitude must be exactly float64.
	if _, ok := m["magnitude"].(float64); !ok {
		t.Errorf("magnitude is %T, must be float64 — DTL silently zeroes "+
			"anything else", m["magnitude"])
	}
}

func TestDTLRoundTrip(t *testing.T) {
	t.Parallel()

	base := MustNewBase(100, 138)

	pu, err := NewPerUnit(1.02, PerUnitVoltage, base)
	if err != nil {
		t.Fatalf("NewPerUnit: %v", err)
	}

	for _, original := range []Quantity{
		Megawatts(150),
		Megavars(-40),
		Kilovolts(138),
		DegreesC(25),
		Ratios(0.95),
		pu,
	} {
		m := original.ToDTL()

		got, err := FromDTL(m)
		if err != nil {
			t.Errorf("FromDTL(%v): %v", m, err)

			continue
		}

		if got.Magnitude() != original.Magnitude() || got.Unit() != original.Unit() {
			t.Errorf("round trip: %s -> %v -> %s", original, m, got)
		}

		if !got.Base().Equal(original.Base()) {
			t.Errorf("round trip lost the base for %s", original)
		}
	}
}

// DTL's own toFloat returns 0 for an unrecognized type without erroring, which
// is how a Quantity struct passed into a formula becomes a silent zero. Our
// reader must refuse instead.
func TestFromDTLRefusesNonNumericMagnitude(t *testing.T) {
	t.Parallel()

	_, err := FromDTL(map[string]any{
		DTLKeyMagnitude: "150",
		DTLKeyUnit:      "MW",
		DTLKeyBase:      "",
	})
	if err == nil {
		t.Error("accepted a string magnitude; coercing it is what produces the " +
			"silent zeroes this wrapper exists to prevent")
	}
}

func TestFromDTLAcceptsIntegerMagnitudes(t *testing.T) {
	t.Parallel()

	// An expression language produces int64 for a literal like 150.
	got, err := FromDTL(map[string]any{
		DTLKeyMagnitude: int64(150),
		DTLKeyUnit:      "MW",
	})
	if err != nil {
		t.Fatalf("FromDTL: %v", err)
	}

	if !closeTo(got.Magnitude(), 150, tol) {
		t.Errorf("magnitude = %v, want 150", got.Magnitude())
	}
}

func TestFromDTLRejectsMissingFields(t *testing.T) {
	t.Parallel()

	if _, err := FromDTL(map[string]any{DTLKeyUnit: "MW"}); err == nil {
		t.Error("accepted a map with no magnitude")
	}

	if _, err := FromDTL(map[string]any{DTLKeyMagnitude: 1.0}); err == nil {
		t.Error("accepted a map with no unit")
	}
}

// A per-unit value whose base went missing in transit must not silently become
// an unscaled number.
func TestFromDTLRejectsPerUnitWithoutABase(t *testing.T) {
	t.Parallel()

	_, err := FromDTL(map[string]any{
		DTLKeyMagnitude: 1.02,
		DTLKeyUnit:      "pu:V",
		DTLKeyBase:      "",
	})
	if err == nil {
		t.Error("rebuilt a per-unit quantity with no base")
	}
}

func TestJSONRoundTrip(t *testing.T) {
	t.Parallel()

	base := MustNewBase(100, 138)

	pu, err := NewPerUnit(1.02, PerUnitImpedance, base)
	if err != nil {
		t.Fatalf("NewPerUnit: %v", err)
	}

	for _, original := range []Quantity{Megawatts(150), Kilovolts(138), pu} {
		data, err := json.Marshal(original)
		if err != nil {
			t.Errorf("Marshal(%s): %v", original, err)

			continue
		}

		var got Quantity
		if err := json.Unmarshal(data, &got); err != nil {
			t.Errorf("Unmarshal(%s): %v", data, err)

			continue
		}

		if got.Magnitude() != original.Magnitude() || got.Unit() != original.Unit() {
			t.Errorf("round trip: %s -> %s -> %s", original, data, got)
		}

		if !got.Base().Equal(original.Base()) {
			t.Errorf("round trip lost the base for %s", original)
		}
	}
}

// The JSON shape mirrors the DTL keys deliberately, so a quantity reads
// identically in an API response and in a formula.
func TestJSONShapeMatchesDTL(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(Megawatts(150))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if got, want := string(data), `{"magnitude":150,"unit":"MW"}`; got != want {
		t.Errorf("JSON = %s, want %s", got, want)
	}
}

func TestUnmarshalJSONRejectsBadInput(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{
		`{"magnitude":1,"unit":"nonsense"}`,
		`{"magnitude":1,"unit":"pu:V"}`,
		`{"magnitude":1,"unit":"MW","base":"garbage"}`,
		`not json`,
	} {
		var q Quantity
		if err := json.Unmarshal([]byte(bad), &q); err == nil {
			t.Errorf("Unmarshal(%s) succeeded", bad)
		}
	}
}
