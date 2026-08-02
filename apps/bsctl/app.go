// Package bsctl assembles the command-line application.
//
// It shares internal/server with the server binary, so a command and a request
// see the same extensions, the same entities and the same storage. Wiring them
// separately would give a CLI that quietly disagrees with the server about
// what the system contains — for a tool whose job is importing and inspecting
// a network model, the worst divergence available.
package bsctl

import (
	"fmt"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/cli"

	"github.com/xraph/blackstart/apps/blackstart"
	"github.com/xraph/blackstart/internal/entities"
	"github.com/xraph/blackstart/internal/server"
	"github.com/xraph/blackstart/pkg/bitemporal"
)

// Name is the CLI's own name, distinct from the server's.
const Name = "bsctl"

// App builds the application a storage-backed command runs against.
//
// It is called by the commands that need it rather than wired as the CLI's
// AppProvider, because that provider is invoked for every command — including
// `entities` and `version`, which need no database at all. Requiring a DSN to
// list what the system models would be a poor first experience and is not
// necessary.
//
// The CLI does not run the outbox worker. A short-lived command should not
// take over draining a shared queue and then exit mid-batch, leaving the next
// replica to wait out an advisory lock held by a process that no longer
// exists.
func App() (forge.App, error) {
	cfg := server.FromEnv(server.DefaultConfig(Name))
	cfg.RunWorker = false
	cfg.HTTPAddress = ""

	return server.Build(cfg, blackstart.Contributors...)
}

// TemporalFlags are the two flags every read command accepts.
//
// They are the reason this CLI has the shape it does: any inspection can be
// asked as of a past moment, using only what was known then. They are attached
// per command rather than globally so that they appear in each command's own
// help, where someone is actually looking for them.
func TemporalFlags() []cli.Flag {
	return []cli.Flag{
		cli.NewStringFlag("valid-at", "", "read the world as it was at this RFC 3339 time", ""),
		cli.NewStringFlag("known-at", "", "read using only what was known at this RFC 3339 time", ""),
	}
}

// Commands returns the domain commands this CLI adds beyond the ones Forge
// provides for free — serve, migrate, health, info and extensions.
func Commands() []cli.Command {
	return []cli.Command{entitiesCommand()}
}

// entitiesCommand lists what the system models.
func entitiesCommand() cli.Command {
	return cli.NewCommand(
		"entities",
		"List the registered entities and the planes they project to",
		func(ctx cli.CommandContext) error {
			reg, err := entities.Build(blackstart.Contributors...)
			if err != nil {
				return err
			}

			inventory := entities.Inventory(reg)
			if inventory == "" {
				ctx.Println("no entities registered yet")

				return nil
			}

			ctx.Printf("%s", inventory)

			return nil
		},
	)
}

// TemporalContext turns the two global flags into a read context.
//
// Supplying --valid-at without --known-at reads a past moment with present
// knowledge, which is right for a report and wrong for scoring a model. It
// warns rather than silently choosing: the distinction is the whole point of
// carrying two axes, and someone who wanted the other one should learn it here
// rather than from a suspiciously good backtest.
func TemporalContext(ctx cli.CommandContext) (bitemporal.AsOf, error) {
	validAt, knownAt := ctx.String("valid-at"), ctx.String("known-at")

	if validAt == "" && knownAt == "" {
		return bitemporal.Current(), nil
	}

	valid := bitemporal.Now()

	if validAt != "" {
		parsed, err := time.Parse(time.RFC3339, validAt)
		if err != nil {
			return bitemporal.AsOf{}, fmt.Errorf("parsing --valid-at: %w", err)
		}

		valid = bitemporal.At(parsed)
	}

	known := bitemporal.Now()

	if knownAt != "" {
		parsed, err := time.Parse(time.RFC3339, knownAt)
		if err != nil {
			return bitemporal.AsOf{}, fmt.Errorf("parsing --known-at: %w", err)
		}

		known = bitemporal.At(parsed)
	}

	if validAt != "" && knownAt == "" {
		ctx.Warning(fmt.Sprintf(
			"reading %s with present knowledge, including corrections filed "+
				"since; pass --known-at for a point-in-time read", valid))
	}

	return bitemporal.PointInTime(valid, known), nil
}
