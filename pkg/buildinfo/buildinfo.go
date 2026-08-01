// Package buildinfo reports the identity of the running binary.
//
// Blackstart's reproducibility rule is that any figure the system produces —
// a risk score, a power-flow solution, a cascade result — must be traceable to
// the dataset version that fed it, the model or solver version that computed
// it, and the code commit that built it. This package supplies the last of
// those three, which is why it exists before anything that produces a number.
//
// Version, Commit, and BuildDate are set at link time by the Makefile:
//
//	-X github.com/xraph/blackstart/pkg/buildinfo.Commit=$(git rev-parse HEAD)
//
// A binary built without those flags reports Unknown rather than lying, and
// callers that record provenance are expected to treat Unknown as a defect in
// the build rather than a normal condition.
package buildinfo

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Unknown is reported for any field the linker did not set.
const Unknown = "unknown"

// Set at link time. Do not assign to these anywhere else.
var (
	// Version is the release version, typically `git describe --tags`.
	Version = Unknown

	// Commit is the full git SHA of the tree the binary was built from.
	Commit = Unknown

	// BuildDate is the UTC build timestamp in RFC 3339 format.
	BuildDate = Unknown
)

// Info describes the running binary.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"buildDate"`
	GoVersion string `json:"goVersion"`
	Platform  string `json:"platform"`

	// Modified reports whether the working tree had uncommitted changes when
	// the binary was built. A result produced by a modified binary is not
	// reproducible from its commit alone, so provenance records carry this.
	Modified bool `json:"modified"`
}

// Get returns the identity of the running binary.
func Get() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		BuildDate: BuildDate,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
		Modified:  vcsModified(),
	}
}

// String renders the identity for logs and `--version` output.
func (i Info) String() string {
	s := fmt.Sprintf("blackstart %s (%s) built %s with %s for %s",
		i.Version, i.Commit, i.BuildDate, i.GoVersion, i.Platform)
	if i.Modified {
		s += " [modified working tree]"
	}

	return s
}

// Reproducible reports whether a result produced by this binary can be
// reproduced from its recorded provenance. It is false when the commit is
// unknown or the working tree was dirty at build time.
func (i Info) Reproducible() bool {
	return i.Commit != Unknown && i.Commit != "" && !i.Modified
}

// vcsModified reads the vcs.modified stamp Go embeds automatically. This is
// independent of the linker flags: `go build` records it whether or not the
// Makefile was used, so a hand-built binary still reports honestly.
func vcsModified() bool {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return false
	}

	for _, s := range info.Settings {
		if s.Key == "vcs.modified" {
			return s.Value == "true"
		}
	}

	return false
}
