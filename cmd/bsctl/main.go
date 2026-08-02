// Command bsctl is the platform's command-line interface.
//
// Thin by design: the commands live in apps/bsctl, which shares its wiring
// with the server so the two cannot drift apart in what they believe the
// system contains.
package main

import (
	"fmt"
	"os"

	"github.com/xraph/forge/cli"

	app "github.com/xraph/blackstart/apps/bsctl"
	"github.com/xraph/blackstart/pkg/buildinfo"
)

func main() {
	// No AppProvider. Forge calls it for every command, including ones that
	// need no storage at all, so wiring it here would make `bsctl entities`
	// demand a database connection to list what the system models. Commands
	// that need an application build one themselves.
	c := cli.New(cli.Config{
		Name:        app.Name,
		Version:     buildinfo.Get().Version,
		Description: "Import, inspect and solve blackstart network models",
	})

	for _, cmd := range app.Commands() {
		if err := c.AddCommand(cmd); err != nil {
			fmt.Fprintf(os.Stderr, "bsctl: registering %s: %v\n", cmd.Name(), err)
			os.Exit(1)
		}
	}

	if err := c.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "bsctl: %v\n", err)
		os.Exit(cli.GetExitCode(err))
	}
}
