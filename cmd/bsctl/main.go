// Command bsctl is the platform's command-line interface: import, inspect,
// migrate, and later solve and replay.
//
// It shares internal/app with the server, so a command and a request see the
// same extensions, the same entities, and the same storage.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/xraph/blackstart/internal/app"
	"github.com/xraph/blackstart/internal/entities"
	"github.com/xraph/blackstart/pkg/bitemporal"
	"github.com/xraph/blackstart/pkg/buildinfo"
)

// contributors mirrors the server's list. Both binaries must see the same
// entities or the CLI would be inspecting a different system than it writes
// to.
//
//nolint:gochecknoglobals // the composition manifest, read once at startup
var contributors = []entities.Contributor{}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "bsctl: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()

		return errors.New("no command given")
	}

	// Global flags every command accepts. The two temporal ones are the whole
	// reason bsctl exists in the shape it does: any inspection can be asked as
	// of a past moment, using only what was known then.
	fs := flag.NewFlagSet("bsctl", flag.ContinueOnError)
	validAt := fs.String("valid-at", "", "read the world as it was at this RFC 3339 time")
	knownAt := fs.String("known-at", "", "read using only what was known at this RFC 3339 time")

	command := args[0]
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	asOf, err := resolveAsOf(*validAt, *knownAt)
	if err != nil {
		return err
	}

	ctx, err := bitemporal.WithAsOf(context.Background(), asOf)
	if err != nil {
		return err
	}

	switch command {
	case "version":
		fmt.Println(buildinfo.Get())

		return nil

	case "entities":
		return listEntities(ctx)

	case "help", "-h", "--help":
		usage()

		return nil

	default:
		usage()

		return fmt.Errorf("unknown command %q", command)
	}
}

// resolveAsOf turns the two global flags into a temporal context.
//
// Supplying --valid-at without --known-at reads a past moment with present
// knowledge, which is right for a report and wrong for scoring a model. The
// warning says so rather than silently choosing: the distinction is the whole
// point of carrying two axes, and a user who wanted the other one should find
// out here rather than from a suspiciously good backtest.
func resolveAsOf(validAt, knownAt string) (bitemporal.AsOf, error) {
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

	asOf := bitemporal.PointInTime(valid, known)

	if validAt != "" && knownAt == "" {
		fmt.Fprintf(os.Stderr,
			"bsctl: reading %s with present knowledge, including corrections "+
				"filed since. Pass --known-at for a point-in-time read.\n", valid)
	}

	return asOf, nil
}

// listEntities takes a context it does not yet read. Every command below this
// point will resolve entities as of the temporal context, and threading it
// from the start means adding the first such command does not also mean
// re-plumbing the dispatch.
func listEntities(_ context.Context) error {
	reg, err := app.Registry(contributors...)
	if err != nil {
		return err
	}

	inventory := entities.Inventory(reg)
	if strings.TrimSpace(inventory) == "" {
		fmt.Println("no entities registered yet")

		return nil
	}

	fmt.Print(inventory)

	return nil
}

func usage() {
	fmt.Fprint(os.Stderr, `bsctl — blackstart command line

Usage:
  bsctl <command> [flags]

Commands:
  entities    list the registered entities and the planes they project to
  version     print the build identity
  help        show this message

Global flags:
  --valid-at   RFC 3339 time: read the world as it was then
  --known-at   RFC 3339 time: read using only what was known then

Reading a past moment without --known-at uses present knowledge, including
corrections filed since. That is right for a report and wrong for scoring a
model.
`)
}
