package quantity

import "testing"

// Every unit in the vocabulary must survive UnitName -> ParseUnit. A unit that
// serializes but cannot be read back is a value that can be written to the API
// and never loaded again.
func TestEveryUnitRoundTripsThroughItsName(t *testing.T) {
	t.Parallel()

	for _, u := range allUnits {
		name := UnitName(u)

		got, err := ParseUnit(name)
		if err != nil {
			t.Errorf("ParseUnit(%q): %v", name, err)

			continue
		}

		if got != u {
			t.Errorf("%s -> %q -> %s: round trip changed the unit", u, name, got)
		}
	}
}

// Per-unit units all carry the symbol "pu", so the serialized name has to
// encode the dimension too or a per-unit voltage and a per-unit impedance
// become indistinguishable once written down.
func TestPerUnitNamesAreDistinct(t *testing.T) {
	t.Parallel()

	seen := map[string]Unit{}

	for _, u := range []Unit{
		PerUnitVoltage, PerUnitCurrent, PerUnitActive, PerUnitReactive,
		PerUnitApparent, PerUnitImpedance, PerUnitAdmittance,
	} {
		name := UnitName(u)
		if prev, dup := seen[name]; dup {
			t.Errorf("%s and %s both serialize as %q", prev, u, name)
		}

		seen[name] = u
	}

	if got := UnitName(PerUnitVoltage); got != "pu:V" {
		t.Errorf("per-unit voltage name = %q, want pu:V", got)
	}

	if got := UnitName(PerUnitActive); got != "pu:W" {
		t.Errorf("per-unit active power name = %q, want pu:W", got)
	}
}

func TestParseUnitRejectsUnknownNames(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{"", "megawatt", "pu", "pu:xyz", "MWh/km"} {
		if _, err := ParseUnit(bad); err == nil {
			t.Errorf("ParseUnit(%q) succeeded", bad)
		}
	}
}

func TestBaseRoundTrip(t *testing.T) {
	t.Parallel()

	for _, b := range []*Base{
		MustNewBase(100, 138),
		MustNewBase(100, 345),
		MustNewBase(1, 0.48),
		MustNewBase(2500, 765),
		MustNewBase(33.3333333, 13.8),
	} {
		s := FormatBase(b)

		got, err := ParseBase(s)
		if err != nil {
			t.Errorf("ParseBase(%q): %v", s, err)

			continue
		}

		if !got.Equal(b) {
			t.Errorf("%s -> %q -> %s: round trip lost precision", b, s, got)
		}
	}
}

func TestParseBaseHandlesEmptyAndMalformed(t *testing.T) {
	t.Parallel()

	got, err := ParseBase("")
	if err != nil {
		t.Errorf("empty base should parse to nil, got %v", err)
	}

	if got != nil {
		t.Errorf("empty base should be nil, got %s", got)
	}

	for _, bad := range []string{"100MVA", "MVA/kV", "abc/138kV", "100MVA/xyz"} {
		if _, err := ParseBase(bad); err == nil {
			t.Errorf("ParseBase(%q) succeeded", bad)
		}
	}
}

func TestFromSI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		si     float64
		target Unit
		want   float64
	}{
		{150e6, Megawatt, 150},
		{138e3, Kilovolt, 138},
		{298.15, Celsius, 25},
		{3600, Hour, 1},
	}

	for _, tt := range tests {
		got, err := FromSI(tt.si, tt.target)
		if err != nil {
			t.Errorf("FromSI(%v, %s): %v", tt.si, tt.target, err)

			continue
		}

		if !closeTo(got.Magnitude(), tt.want, tol) {
			t.Errorf("FromSI(%v, %s) = %v, want %v", tt.si, tt.target, got.Magnitude(), tt.want)
		}
	}
}

// The store mapper writes MustSI and reads FromSI. If those two disagree the
// database drifts silently, so the pairing is checked directly.
func TestFromSIInvertsMustSI(t *testing.T) {
	t.Parallel()

	for _, original := range []Quantity{
		Megawatts(150), Kilovolts(138), DegreesC(25),
		MustNew(2, Hour), Megavars(-40), Amperes(418.37),
	} {
		got, err := FromSI(original.MustSI(), original.Unit())
		if err != nil {
			t.Errorf("FromSI: %v", err)

			continue
		}

		eq, err := original.Equal(got, 1e-12)
		if err != nil {
			t.Errorf("Equal: %v", err)

			continue
		}

		if !eq {
			t.Errorf("persistence round trip: %s -> %v -> %s",
				original, original.MustSI(), got)
		}
	}
}

func TestFromSIRejectsPerUnitTargets(t *testing.T) {
	t.Parallel()

	if _, err := FromSI(138e3, PerUnitVoltage); err == nil {
		t.Error("FromSI accepted a per-unit target; there is no base to scale by")
	}
}
