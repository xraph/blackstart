package testsupport

import (
	"strconv"
	"strings"
	"testing"
)

// describeDifference is the part worth testing directly: it runs at the exact
// moment somebody is trying to work out why a two-thousand-bus model changed,
// and a full diff there is useless.
func TestDescribeDifferenceReportsTheFirstDivergence(t *testing.T) {
	t.Parallel()

	want := []byte("alpha\nbravo\ncharlie\ndelta\n")
	got := []byte("alpha\nbravo\nCHARLIE\ndelta\n")

	desc := describeDifference(want, got)

	if !strings.Contains(desc, "line 3") {
		t.Errorf("should point at line 3:\n%s", desc)
	}

	if !strings.Contains(desc, "charlie") || !strings.Contains(desc, "CHARLIE") {
		t.Errorf("should show both sides:\n%s", desc)
	}
}

func TestDescribeDifferenceShowsContext(t *testing.T) {
	t.Parallel()

	want := []byte("a\nb\nc\nd\ne\nf\ng\n")
	got := []byte("a\nb\nc\nD\ne\nf\ng\n")

	desc := describeDifference(want, got)

	// Two lines either side, so the reader can see where they are.
	for _, line := range []string{"b", "c", "e"} {
		if !strings.Contains(desc, line) {
			t.Errorf("context line %q is missing:\n%s", line, desc)
		}
	}

	// But not the whole file.
	if strings.Contains(desc, "want[7]") {
		t.Errorf("the description ran past the context window:\n%s", desc)
	}
}

// When one side is a strict prefix of the other with no differing line, there
// is no divergence to point at and the report has to say so instead.
func TestDescribeDifferenceHandlesAStrictPrefix(t *testing.T) {
	t.Parallel()

	want := []byte("a\nb")
	got := []byte("a\nb\nc")

	desc := describeDifference(want, got)
	if !strings.Contains(desc, "lengths differ") {
		t.Errorf("a strict prefix should be reported as a length change:\n%s", desc)
	}
}

// An append to a newline-terminated file is not a strict prefix: the trailing
// empty element becomes the new content, so it reports as a differing line.
// That is the more useful message of the two, and worth pinning so a future
// change does not "fix" it into the vaguer one.
func TestDescribeDifferenceReportsAnAppendAsALineChange(t *testing.T) {
	t.Parallel()

	desc := describeDifference([]byte("a\nb\n"), []byte("a\nb\nc\n"))

	if !strings.Contains(desc, "line 3") {
		t.Errorf("an append should point at the first changed line:\n%s", desc)
	}
}

// A single enormous line — a serialized network model on one line, say —
// must not dump megabytes into the test output.
func TestDescribeDifferenceTruncatesLongLines(t *testing.T) {
	t.Parallel()

	want := []byte(strings.Repeat("x", 5000))
	got := []byte(strings.Repeat("y", 5000))

	desc := describeDifference(want, got)

	if len(desc) > 1000 {
		t.Errorf("description is %d bytes; long lines should be truncated", len(desc))
	}

	if !strings.Contains(desc, "5000 bytes") {
		t.Errorf("truncation should report the real length:\n%s", desc)
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	short := "fits"
	if got := truncate(short); got != short {
		t.Errorf("truncate(%q) = %q", short, got)
	}

	long := strings.Repeat("z", maxLineWidth+50)

	got := truncate(long)
	if len(got) >= len(long) {
		t.Errorf("truncate did not shorten a %d-byte line", len(long))
	}

	if !strings.Contains(got, strconv.Itoa(len(long))) {
		t.Errorf("truncate should report the original length: %q", got)
	}
}

func TestGoldenRoundTrip(t *testing.T) {
	t.Parallel()

	// Written by -update; committed so the comparison path is exercised in
	// ordinary runs too.
	GoldenString(t, "example", "alpha\nbravo\ncharlie\n")
}

func TestGoldenJSON(t *testing.T) {
	t.Parallel()

	GoldenJSON(t, "example_json", map[string]any{
		"buses":    2,
		"name":     "two-bus",
		"baseMVA":  100,
		"balanced": true,
	})
}
