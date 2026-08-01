// Package id provides type-prefixed identifiers for every entity in the
// platform.
//
// An identifier carries its own type: `bus_01j9x8k2m3n4p5q6r7s8t9v0w1` is
// self-evidently a bus, in a log line, a URL, a support ticket, or a CSV a
// researcher opened in a spreadsheet. That property is worth more here than
// usual, because the entities are numerous and similarly shaped — a
// substation, a bay, a connectivity node and a terminal are all opaque
// identifiers otherwise, and confusing two of them produces a network model
// that is subtly wrong rather than obviously broken.
//
// Prefixes are declared once and are immutable thereafter. Changing a shipped
// prefix invalidates every stored identifier, every external alias that
// resolved to it, and every provenance record naming it. The registry in
// registry.go makes that constraint mechanical rather than a matter of
// discipline.
//
// The underlying implementation is the TypeID specification, whose suffix is a
// UUIDv7 in Crockford base32 — so identifiers sort by creation time, which is
// useful in a system that records when it learned things.
package id

import (
	"database/sql/driver"
	"fmt"

	"go.jetify.com/typeid/v2"
)

// ID is a type-prefixed identifier.
//
// The zero value is Nil: a valid, comparable, storable "no identifier" that is
// distinguishable from a real one. That matters because a foreign key to
// nothing is common in this domain — a bus with no parent bay, a branch whose
// far end is outside the modelled area.
//
// Methods use value receivers, because an identifier is an immutable value.
// UnmarshalText and Scan are the exceptions: neither interface can be
// satisfied without a pointer receiver.
//
//nolint:recvcheck // UnmarshalText and Scan must take a pointer
type ID struct {
	inner typeid.TypeID
}

// Nil is the zero identifier.
var Nil ID

// New generates a fresh identifier with the given prefix.
func New(p Prefix) (ID, error) {
	if err := p.Validate(); err != nil {
		return Nil, err
	}

	inner, err := typeid.Generate(string(p))
	if err != nil {
		return Nil, fmt.Errorf("id: generating a %q identifier: %w", p, err)
	}

	return ID{inner: inner}, nil
}

// MustNew is New for prefixes that are compile-time constants, which is all of
// them. It panics rather than returning an error, because a malformed constant
// prefix is a programming error that should stop the process at once.
func MustNew(p Prefix) ID {
	v, err := New(p)
	if err != nil {
		panic(err)
	}

	return v
}

// Parse reads an identifier of any type.
//
// Prefer ParseWithPrefix wherever the expected type is known — which is nearly
// everywhere, since a handler for /buses/{id} knows it wants a bus. Accepting
// any prefix there would let a substation identifier reach a bus lookup and
// fail as "not found" rather than as "wrong type".
func Parse(s string) (ID, error) {
	inner, err := typeid.Parse(s)
	if err != nil {
		return Nil, fmt.Errorf("id: parsing %q: %w", s, err)
	}

	return ID{inner: inner}, nil
}

// ParseWithPrefix reads an identifier and requires it to be of the given type.
func ParseWithPrefix(s string, expected Prefix) (ID, error) {
	parsed, err := Parse(s)
	if err != nil {
		return Nil, err
	}

	if got := parsed.Prefix(); got != expected {
		return Nil, fmt.Errorf(
			"id: expected a %q identifier, got %q (%s)", expected, got, s)
	}

	return parsed, nil
}

// MustParse is Parse for literals in tests and fixtures.
func MustParse(s string) ID {
	v, err := Parse(s)
	if err != nil {
		panic(err)
	}

	return v
}

// Prefix returns the entity type this identifier names.
func (i ID) Prefix() Prefix { return Prefix(i.inner.Prefix()) }

// IsZero reports whether this is the Nil identifier.
func (i ID) IsZero() bool { return i.inner.IsZero() }

// String returns the canonical text form, or "" for Nil.
func (i ID) String() string {
	if i.IsZero() {
		return ""
	}

	return i.inner.String()
}

// MarshalText implements encoding.TextMarshaler, which also covers JSON.
func (i ID) MarshalText() ([]byte, error) {
	if i.IsZero() {
		return []byte{}, nil
	}

	return i.inner.MarshalText()
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (i *ID) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		*i = Nil

		return nil
	}

	parsed, err := Parse(string(text))
	if err != nil {
		return err
	}

	*i = parsed

	return nil
}

// Value implements driver.Valuer. Identifiers are stored as text, matching
// Fabriq's `id TEXT PRIMARY KEY` contract. Nil stores as SQL NULL, so an
// absent foreign key is absent rather than an empty string.
func (i ID) Value() (driver.Value, error) {
	if i.IsZero() {
		// A nil driver.Value is SQL NULL, which is the correct Valuer
		// contract for an absent identifier.
		return nil, nil //nolint:nilnil // nil,nil is how Valuer spells NULL
	}

	return i.inner.String(), nil
}

// Scan implements sql.Scanner.
func (i *ID) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*i = Nil

		return nil
	case string:
		return i.UnmarshalText([]byte(v))
	case []byte:
		return i.UnmarshalText(v)
	default:
		return fmt.Errorf("id: cannot scan %T into an identifier", src)
	}
}
