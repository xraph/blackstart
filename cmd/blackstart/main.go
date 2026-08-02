// Command blackstart is the platform host.
//
// It loads every extension and serves the HTTP and streaming surface. The
// wiring itself lives in internal/app, shared with bsctl, so that the two
// binaries cannot drift apart in what they believe the system contains.
package main

import (
	"fmt"
	"os"

	"github.com/xraph/blackstart/internal/app"
	"github.com/xraph/blackstart/internal/entities"
	"github.com/xraph/blackstart/pkg/buildinfo"
)

// contributors lists every extension that declares entities.
//
// It is a variable rather than being inlined so that the set is visible in one
// place: this slice is the answer to "what does this system model", and it
// should be readable without tracing constructor calls.
//
//nolint:gochecknoglobals // the composition manifest, read once at startup
var contributors = []entities.Contributor{
	// Extensions register here as they are built. The first is `datasource`,
	// the public data source registry, which is also the extension that proves
	// the seven-stage contract end to end.
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "blackstart: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := app.FromEnv(app.DefaultConfig("blackstart"))

	// The server listens and drains the outbox; the CLI does neither.
	if cfg.HTTPAddress == "" {
		cfg.HTTPAddress = ":8080"
	}

	cfg.RunWorker = true

	built, err := app.Build(cfg, contributors...)
	if err != nil {
		return err
	}

	info := buildinfo.Get()
	if !info.Reproducible() {
		// Not fatal — development builds are normal — but a result produced by
		// this binary cannot be reproduced from its commit alone, and the
		// provenance records will say so. Better to know at startup.
		fmt.Fprintf(os.Stderr,
			"blackstart: warning: this build is not reproducible (%s); "+
				"results it produces will be recorded as such\n", info)
	}

	return built.Run()
}
