package matpower_test

import (
	"strings"
	"testing"

	"github.com/xraph/blackstart/extensions/networkmodel/matpower"
)

// A file that cannot be read is better than one read wrong: a network with a
// transposed column still solves and still produces numbers. Every rejection
// names what was missing.
func TestAMalformedCaseIsRefusedWithItsCause(t *testing.T) {
	cases := map[string]struct {
		source   string
		mentions string
	}{
		"no base": {
			source:   strings.ReplaceAll(threeBus, "mpc.baseMVA = 100;", ""),
			mentions: "baseMVA",
		},
		"negative base": {
			source:   strings.ReplaceAll(threeBus, "mpc.baseMVA = 100;", "mpc.baseMVA = -1;"),
			mentions: "positive",
		},
		"no bus matrix": {
			source:   strings.Split(threeBus, "mpc.bus = [")[0] + "mpc.branch = [\n1 2 0 0 0 0 0 0 0 0 1;\n];",
			mentions: "mpc.bus",
		},
		"short row": {
			source: strings.Replace(threeBus,
				"	3	1	94.2	19.0	0	19	1	1.010	-12.72	138	1	1.06	0.94;",
				"	3	1	94.2;", 1),
			mentions: "columns",
		},
		"branch to nowhere": {
			source: strings.Replace(threeBus,
				"	2	3	0.04699	0.19797	0.0438	150	0	0	0	0	1;",
				"	2	9	0.04699	0.19797	0.0438	150	0	0	0	0	1;", 1),
			mentions: "not in mpc.bus",
		},
		"unknown bus type": {
			source: strings.Replace(threeBus,
				"	3	1	94.2", "	3	7	94.2", 1),
			mentions: "only types",
		},
		"unclosed matrix": {
			source:   strings.Replace(threeBus, "];\n\n%% generator data", "\n\n%% generator data", 1),
			mentions: "never closed",
		},
		"a MATLAB expression": {
			source: strings.Replace(threeBus,
				"	1	3	0	0	0	0	1	1.06	0	138	1	1.06	0.94;",
				"	1	3	0	0	0	0	1	sqrt(2)	0	138	1	1.06	0.94;", 1),
			mentions: "not a number",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := matpower.Parse(tc.source)
			if err == nil {
				t.Fatalf("Parse accepted a case with %s", name)
			}

			if !strings.Contains(err.Error(), tc.mentions) {
				t.Errorf("message %q does not mention %q", err, tc.mentions)
			}
		})
	}
}

// Comments must not be read as data. A `%` starts one, and a case file is
// mostly column headers written as comments.
func TestCommentsAreIgnored(t *testing.T) {
	got := parse(t, threeBus)

	// The bus matrix has three data rows and four comment lines around it.
	if len(got.Model.Buses) != 3 {
		t.Errorf("got %d buses; a comment was read as data", len(got.Model.Buses))
	}
}

func TestLooksLikeCaseIsCheapAndNotAValidation(t *testing.T) {
	if !matpower.LooksLikeCase(threeBus) {
		t.Error("a real case was not recognized")
	}

	if matpower.LooksLikeCase("bus,voltage\n1,1.0\n") {
		t.Error("a CSV was taken for a MATPOWER case")
	}

	// It answers "should I try", not "is it valid" — a file with the marker
	// and nothing else passes here and fails in Parse.
	if !matpower.LooksLikeCase("mpc.baseMVA = 100;") {
		t.Error("the marker alone should be enough to try")
	}
}
