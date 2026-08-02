// Package testsupport holds the shared test harness: golden files, a real
// dependency-injection container, and the containerized database fixtures.
//
// It is under internal/ because it is not part of the platform's API, and it
// is a normal package rather than a _test package because it is imported by
// tests across many packages.
package testsupport

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// update rewrites golden files instead of comparing against them:
//
//	go test ./extensions/scl/... -update
//
// Review the resulting diff before committing. A golden file regenerated
// without reading the diff records whatever the code did, which is the
// opposite of what a golden file is for.
var update = flag.Bool("update", false, "rewrite golden files instead of comparing")

// Golden compares got against testdata/<name>.golden.
//
// The path resolves relative to the calling package, because `go test` runs
// each test binary with its own package directory as the working directory.
func Golden(t *testing.T, name string, got []byte) {
	t.Helper()

	path := filepath.Join("testdata", name+".golden")

	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("creating %s: %v", filepath.Dir(path), err)
		}

		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}

		t.Logf("wrote %s (%d bytes) — review the diff before committing", path, len(got))

		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("%s does not exist; run the test with -update to create it, "+
				"then read the diff before committing", path)
		}

		t.Fatalf("reading %s: %v", path, err)
	}

	if bytes.Equal(got, want) {
		return
	}

	t.Errorf("output differs from %s\n%s", path, describeDifference(want, got))
}

// GoldenJSON compares a value against a golden file as indented JSON.
//
// Indented and key-sorted, so that a change shows up as a small diff rather
// than as one enormous altered line. Go's encoder sorts map keys already;
// struct field order is declaration order, which is stable.
func GoldenJSON(t *testing.T, name string, v any) {
	t.Helper()

	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshaling for golden comparison: %v", err)
	}

	Golden(t, name, append(got, '\n'))
}

// GoldenString compares a string against a golden file.
func GoldenString(t *testing.T, name, got string) {
	t.Helper()

	Golden(t, name, []byte(got))
}

// describeDifference renders the first differing line with a little context.
//
// A full diff of a two-thousand-bus network model tells the reader nothing
// they can act on. The first divergence, with line numbers, is almost always
// enough to identify the cause.
func describeDifference(want, got []byte) string {
	wantLines := strings.Split(string(want), "\n")
	gotLines := strings.Split(string(got), "\n")

	limit := min(len(wantLines), len(gotLines))

	for i := range limit {
		if wantLines[i] == gotLines[i] {
			continue
		}

		var b strings.Builder

		b.WriteString("first difference at line ")
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(":\n")

		for j := max(0, i-2); j < min(limit, i+3); j++ {
			marker := "  "
			if j == i {
				marker = "> "
			}

			b.WriteString(marker + "want[" + strconv.Itoa(j+1) + "]: " + truncate(wantLines[j]) + "\n")
			b.WriteString(marker + " got[" + strconv.Itoa(j+1) + "]: " + truncate(gotLines[j]) + "\n")
		}

		return b.String()
	}

	return "contents match for " + strconv.Itoa(limit) + " lines, but lengths differ: " +
		"want " + strconv.Itoa(len(wantLines)) + " lines, got " + strconv.Itoa(len(gotLines))
}

const maxLineWidth = 160

func truncate(s string) string {
	if len(s) <= maxLineWidth {
		return s
	}

	return s[:maxLineWidth] + "... (" + strconv.Itoa(len(s)) + " bytes)"
}
