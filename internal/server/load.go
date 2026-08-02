package server

import (
	"fmt"
	"io"
	"strings"
)

// Load builds the configuration for a binary: defaults, then files, then the
// environment.
//
// The one entry point both binaries use, so neither can accidentally read a
// different set of layers than the other. It returns the files it read for the
// same reason FromFile does — a result is only reproducible if you can say
// what configured the process that produced it.
func Load(name string) (Config, []string, error) {
	cfg, loaded, err := FromFile(DefaultConfig(name))
	if err != nil {
		return cfg, loaded, err
	}

	return FromEnv(cfg), loaded, nil
}

// ReportConfigSources writes the configuration files that were read.
//
// It goes to stderr rather than through the logger because it happens before
// the application — and therefore the logger — exists. A blank line when
// nothing was found is deliberate: "no config file" is the normal container
// case and does not deserve a warning, but the absence of the line is itself
// the signal that only defaults and environment variables are in play.
func ReportConfigSources(w io.Writer, loaded []string) {
	if len(loaded) == 0 {
		return
	}

	fmt.Fprintf(w, "blackstart: configuration from %s\n", strings.Join(loaded, ", "))
}
