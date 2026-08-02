// Package blackstart assembles the server application.
//
// The Forge layout puts the binary's entry point in cmd/ and its assembly
// here, so main stays a handful of lines and the wiring is importable — by
// tests, by the CLI, and by anything else that needs a configured application
// without starting a process.
package blackstart

import (
	"os"

	"github.com/xraph/forge"

	"github.com/xraph/blackstart/internal/entities"
	"github.com/xraph/blackstart/internal/server"
)

// Name is how this application identifies itself in logs and health output.
const Name = "blackstart"

// Contributors lists every extension that declares entities.
//
// This slice is the answer to "what does this system model", and it is
// exported so the CLI sees exactly the same set. Two binaries with two lists
// is how a CLI ends up inspecting a different system than it writes to.
//
//nolint:gochecknoglobals // the composition manifest, read once at startup
var Contributors = []entities.Contributor{
	// Extensions register here as they are built.
}

// New builds the server application.
//
// The server listens and drains the outbox; the CLI does neither. That is the
// only difference between the two, and it lives here rather than in main.
func New() (forge.App, error) {
	cfg, loaded, err := server.Load(Name)
	if err != nil {
		return nil, err
	}

	// Announced rather than merely available: "which configuration was in
	// effect" is one of the inputs a result has to be traceable to, and the
	// moment to record it is before anything connects, not after someone asks
	// why two runs disagree.
	server.ReportConfigSources(os.Stderr, loaded)

	if cfg.HTTPAddress == "" {
		cfg.HTTPAddress = ":8080"
	}

	// The server drains the outbox. Nothing else does, and the relay is not
	// optional — an outbox nobody reads is a graph projection that silently
	// stops advancing while every write still succeeds.
	cfg.RunWorker = true

	return server.Build(cfg, Contributors...)
}
