package id

import (
	"strings"
	"testing"
)

func TestPrefixValidation(t *testing.T) {
	t.Parallel()

	valid := []Prefix{"bus", "sub", "vlvl", "network_model", "a", "ab"}
	for _, p := range valid {
		if err := p.Validate(); err != nil {
			t.Errorf("%q should be valid: %v", p, err)
		}
	}

	invalid := []Prefix{
		"",
		"Bus",                           // uppercase
		"bus1",                          // digit
		"_bus",                          // leading underscore
		"bus_",                          // trailing underscore
		"bus-bar",                       // hyphen
		"bus bar",                       // space
		Prefix(strings.Repeat("a", 64)), // too long
	}

	for _, p := range invalid {
		if err := p.Validate(); err == nil {
			t.Errorf("%q should be rejected", p)
		}
	}
}

func TestNewRejectsAMalformedPrefix(t *testing.T) {
	t.Parallel()

	if _, err := New("Bus"); err == nil {
		t.Error("generated an identifier with an uppercase prefix")
	}
}
