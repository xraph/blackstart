package id

import (
	"database/sql/driver"
	"encoding/json"
	"strings"
	"testing"
)

const busPrefix Prefix = "bus"

func TestNewProducesAPrefixedIdentifier(t *testing.T) {
	t.Parallel()

	got, err := New(busPrefix)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if got.Prefix() != busPrefix {
		t.Errorf("prefix = %q, want %q", got.Prefix(), busPrefix)
	}

	if !strings.HasPrefix(got.String(), "bus_") {
		t.Errorf("%s does not read as a bus identifier", got)
	}

	if got.IsZero() {
		t.Error("a generated identifier reported itself as Nil")
	}
}

func TestNewProducesDistinctIdentifiers(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{}, 1000)

	for range 1000 {
		v := MustNew(busPrefix)
		if _, dup := seen[v.String()]; dup {
			t.Fatalf("generated a duplicate identifier: %s", v)
		}

		seen[v.String()] = struct{}{}
	}
}

// The TypeID suffix is a UUIDv7, so identifiers issued in order sort in order.
// A system that records when it learned things gets chronological ordering
// from its primary keys for free, and it is worth knowing if that stops being
// true.
func TestIdentifiersSortByCreationTime(t *testing.T) {
	t.Parallel()

	first := MustNew(busPrefix)
	second := MustNew(busPrefix)

	if first.String() >= second.String() {
		t.Errorf("identifiers do not sort chronologically: %s >= %s", first, second)
	}
}

// A handler for /buses/{id} knows it wants a bus. Accepting any prefix there
// lets a substation identifier fail as "not found" rather than "wrong type",
// which sends the reader looking in the wrong place.
func TestParseWithPrefixRejectsTheWrongType(t *testing.T) {
	t.Parallel()

	substation := MustNew("sub")

	if _, err := ParseWithPrefix(substation.String(), busPrefix); err == nil {
		t.Fatal("a substation identifier was accepted as a bus")
	}

	bus := MustNew(busPrefix)

	got, err := ParseWithPrefix(bus.String(), busPrefix)
	if err != nil {
		t.Fatalf("ParseWithPrefix: %v", err)
	}

	if got != bus {
		t.Errorf("round trip changed the identifier: %s != %s", got, bus)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{
		"",
		"bus",
		"bus_",
		"not an id",
		"bus_notvalidbase32!!!",
		"BUS_01j9x8k2m3n4p5q6r7s8t9v0w1",
	} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) succeeded", bad)
		}
	}
}

func TestNilIsUsableAndDistinct(t *testing.T) {
	t.Parallel()

	if !Nil.IsZero() {
		t.Error("Nil is not zero")
	}

	if Nil.String() != "" {
		t.Errorf("Nil renders as %q, want empty", Nil.String())
	}

	if MustNew(busPrefix).IsZero() {
		t.Error("a real identifier reported itself as Nil")
	}

	// Comparable, so it works as a map key and in an == check.
	var other ID
	if other != Nil {
		t.Error("the zero value should equal Nil")
	}
}

func TestTextRoundTrip(t *testing.T) {
	t.Parallel()

	original := MustNew(busPrefix)

	text, err := original.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText: %v", err)
	}

	var got ID
	if err := got.UnmarshalText(text); err != nil {
		t.Fatalf("UnmarshalText: %v", err)
	}

	if got != original {
		t.Errorf("round trip: %s -> %s -> %s", original, text, got)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	t.Parallel()

	type payload struct {
		Bus    ID `json:"bus"`
		Parent ID `json:"parent"`
	}

	original := payload{Bus: MustNew(busPrefix)}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got payload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Bus != original.Bus {
		t.Errorf("bus round trip: %s -> %s", original.Bus, got.Bus)
	}

	if !got.Parent.IsZero() {
		t.Errorf("an absent identifier came back as %s", got.Parent)
	}
}

// An absent foreign key must be SQL NULL, not an empty string. A branch whose
// far end is outside the modelled area is common here, and "" would sort and
// join as a real value.
func TestValueUsesNullForNil(t *testing.T) {
	t.Parallel()

	v, err := Nil.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}

	if v != nil {
		t.Errorf("Nil stored as %#v, want SQL NULL", v)
	}

	issued := MustNew(busPrefix)

	v, err = issued.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}

	if got, ok := v.(string); !ok || got != issued.String() {
		t.Errorf("stored as %#v, want the text form %q", v, issued.String())
	}
}

func TestScan(t *testing.T) {
	t.Parallel()

	original := MustNew(busPrefix)

	tests := []struct {
		name string
		src  any
		want ID
	}{
		{"nil becomes Nil", nil, Nil},
		{"string", original.String(), original},
		{"bytes", []byte(original.String()), original},
		{"empty string becomes Nil", "", Nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got ID
			if err := got.Scan(tt.src); err != nil {
				t.Fatalf("Scan(%#v): %v", tt.src, err)
			}

			if got != tt.want {
				t.Errorf("Scan(%#v) = %s, want %s", tt.src, got, tt.want)
			}
		})
	}
}

func TestScanRejectsOtherTypes(t *testing.T) {
	t.Parallel()

	var got ID
	if err := got.Scan(42); err == nil {
		t.Error("scanned an integer into an identifier")
	}
}

// Compile-time proof that the database and encoding contracts are satisfied.
var (
	_ driver.Valuer = ID{}
	_ interface {
		Scan(src any) error
	} = (*ID)(nil)
)
