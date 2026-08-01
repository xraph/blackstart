package buildinfo

import (
	"strings"
	"testing"
)

func TestGetReportsRuntimeFacts(t *testing.T) {
	t.Parallel()

	got := Get()

	if got.GoVersion == "" {
		t.Error("GoVersion is empty; it should come from runtime.Version()")
	}

	if got.Platform == "" {
		t.Error("Platform is empty; it should be GOOS/GOARCH")
	}
}

func TestUnsetFieldsReportUnknownRatherThanEmpty(t *testing.T) {
	t.Parallel()

	// The package defaults matter: a binary built without the Makefile must
	// say so rather than reporting an empty string that a provenance record
	// would happily store as if it were a real commit.
	for name, value := range map[string]string{
		"Version":   Version,
		"Commit":    Commit,
		"BuildDate": BuildDate,
	} {
		if value == "" {
			t.Errorf("%s is empty; it should default to %q", name, Unknown)
		}
	}
}

func TestReproducible(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		info Info
		want bool
	}{
		{
			name: "known commit and clean tree is reproducible",
			info: Info{Commit: "6f1c2a9", Modified: false},
			want: true,
		},
		{
			name: "unknown commit is not reproducible",
			info: Info{Commit: Unknown, Modified: false},
			want: false,
		},
		{
			name: "empty commit is not reproducible",
			info: Info{Commit: "", Modified: false},
			want: false,
		},
		{
			name: "modified working tree is not reproducible even with a commit",
			info: Info{Commit: "6f1c2a9", Modified: true},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.info.Reproducible(); got != tt.want {
				t.Errorf("Reproducible() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStringFlagsModifiedTree(t *testing.T) {
	t.Parallel()

	clean := Info{Version: "v0.1.0", Commit: "abc", BuildDate: "now", Modified: false}
	if got := clean.String(); strings.Contains(got, "modified") {
		t.Errorf("clean build should not mention a modified tree: %q", got)
	}

	dirty := Info{Version: "v0.1.0", Commit: "abc", BuildDate: "now", Modified: true}
	if got := dirty.String(); !strings.Contains(got, "modified") {
		t.Errorf("modified build must say so, so an unreproducible result is "+
			"visible in the logs that recorded it: %q", got)
	}
}
