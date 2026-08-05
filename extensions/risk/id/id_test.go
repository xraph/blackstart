package id

import (
	"testing"

	gid "github.com/xraph/blackstart/extensions/geo/id"
)

func TestAComputationIdentifierRoundTrips(t *testing.T) {
	minted := NewComputation()

	parsed, err := ParseComputation(minted.String())
	if err != nil {
		t.Fatalf("ParseComputation: %v", err)
	}

	if parsed != minted {
		t.Fatalf("parsed %s, minted %s", parsed, minted)
	}
}

func TestAComputationRejectsAnotherPrefix(t *testing.T) {
	if _, err := ParseComputation(gid.NewSpan().String()); err == nil {
		t.Fatal("a span parsed as a computation; the prefix is the type and " +
			"a parser that ignores it makes it decoration")
	}
}
