package parse_test

import (
	"os"
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/scl/parse"
)

// FuzzDecode looks for inputs that panic rather than returning an error.
//
// A parser reading files that arrive from outside the project — a utility's
// SCD, a vendor's ICD — has to fail by saying so. This is a public repository
// and an XML parser is a classic denial-of-service surface, so "it does not
// crash on malformed input" is a property worth asserting mechanically rather
// than by inspection.
func FuzzDecode(f *testing.F) {
	if fixture, err := os.ReadFile("testdata/ring.scd"); err == nil {
		f.Add(string(fixture))
	}

	for _, seed := range []string{
		`<SCL/>`,
		`<SCL><Substation name="A"><VoltageLevel name="B"><Bay name="C"/></VoltageLevel></Substation></SCL>`,
		`<SCL><IED name="I"><AccessPoint name="A"><Server><LDevice inst="L"/></Server></AccessPoint></IED></SCL>`,
		`<SCL><Communication><SubNetwork name="S"><ConnectedAP iedName="I" apName="A"/></SubNetwork></Communication></SCL>`,
		`<!DOCTYPE SCL><SCL/>`,
		`<SCL><Substation name="&amp;"/></SCL>`,
		"<SCL>\x00</SCL>",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, document string) {
		// The result is deliberately unused. What is under test is that this
		// returns at all — any input either decodes or reports why, and
		// neither panics nor runs away.
		_, _ = parse.Decode(strings.NewReader(document))
	})
}
