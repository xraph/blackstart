// Command blackstart is the platform host.
//
// It is deliberately thin. The assembly lives in apps/blackstart so it can be
// imported — by tests, by the CLI, and by anything needing a configured
// application without starting a process.
package main

import (
	"fmt"
	"os"

	"github.com/xraph/forge"
	"github.com/xraph/forge/cli"

	app "github.com/xraph/blackstart/apps/blackstart"
	"github.com/xraph/blackstart/pkg/buildinfo"
)

func main() {
	// RunApp wraps the application in a command surface, so the server binary
	// also carries serve, migrate, health, info and extensions rather than
	// needing a separate tool for operational tasks. Extensions implementing
	// CLICommandProvider contribute their own commands here too.
	cli.RunApp(func(cli.CommandContext) (forge.App, error) {
		warnIfUnreproducible()

		return app.New()
	},
		cli.WithCLIName(app.Name),
		cli.WithCLIVersion(buildinfo.Get().Version),
		cli.WithCLIDescription("Weather-driven outage risk and cascade analysis"),
	)
}

// warnIfUnreproducible reports a build that cannot be traced to a commit.
//
// Not fatal — development builds are normal — but a result this binary
// produces cannot be reproduced from its commit alone, and its provenance
// record will say so. Better known at startup than discovered in a provenance
// query weeks later.
func warnIfUnreproducible() {
	info := buildinfo.Get()
	if info.Reproducible() {
		return
	}

	fmt.Fprintf(os.Stderr,
		"blackstart: warning: this build is not reproducible (%s); "+
			"results it produces will be recorded as such\n", info)
}
