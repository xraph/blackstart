package bsctl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/xraph/forge"
	"github.com/xraph/forge/cli"

	"github.com/xraph/blackstart/extensions/scl"
	"github.com/xraph/blackstart/extensions/scl/device"
)

// sclCommand groups everything to do with IEC 61850 configuration files.
func sclCommand() cli.Command {
	return cli.NewCommand(
		"scl",
		"Import and inspect IEC 61850 substation configuration",
		func(ctx cli.CommandContext) error {
			ctx.Println("usage: bsctl scl <import|devices|listeners>")

			return nil
		},
		cli.WithSubcommand(sclImportCommand()),
		cli.WithSubcommand(sclDevicesCommand()),
		cli.WithSubcommand(sclListenersCommand()),
	)
}

// sclImportCommand reads an SCD or ICD file.
func sclImportCommand() cli.Command {
	return cli.NewCommand(
		"import",
		"Read an SCD or ICD file as topology and a communication graph",
		func(ctx cli.CommandContext) error {
			if ctx.NArgs() < 1 {
				return errors.New("usage: bsctl scl import <file.scd>")
			}

			path := ctx.Args()[0]

			source, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("reading %s: %w", path, err)
			}

			engine, cleanup, err := sclEngine(ctx.Context())
			if err != nil {
				return err
			}
			defer cleanup()

			absolute, err := filepath.Abs(path)
			if err != nil {
				absolute = path
			}

			result, err := engine.Import(ctx.Context(), scl.ImportRequest{
				Source: source,
				URL:    "file://" + absolute,
			})
			if err != nil {
				return err
			}

			ctx.Printf("imported %s\n", filepath.Base(path))
			ctx.Printf("  substations   %d\n", len(result.Substations))
			ctx.Printf("  devices       %d\n", result.Devices)
			ctx.Printf("  publications  %d\n", result.Publications)
			ctx.Printf("  subscriptions %d\n", result.Subscriptions)

			for _, site := range result.Substations {
				ctx.Printf("  %s\n", site)
			}

			// A file where most subscriptions are unresolved is usually a
			// partial export rather than a substation full of external
			// references, and the operator should be able to tell which.
			if result.Unresolved > 0 {
				ctx.Warning(fmt.Sprintf(
					"%d subscription(s) name devices this file does not configure; "+
						"no edge was written for them because an edge needs both ends",
					result.Unresolved))
			}

			for _, skipped := range result.Skipped {
				ctx.Printf("  skipped %s (%s): %s\n", skipped.Name, skipped.Type, skipped.Reason)
			}

			if !result.Traceable {
				ctx.Warning(
					"provenance was not recorded, so this import cannot be traced " +
						"back to the file it came from")
			}

			return nil
		},
		cli.WithUsage("bsctl scl import <file.scd>"),
	)
}

// sclDevicesCommand lists what has been imported.
func sclDevicesCommand() cli.Command {
	return cli.NewCommand(
		"devices",
		"List configured intelligent electronic devices",
		func(ctx cli.CommandContext) error {
			engine, cleanup, err := sclEngine(ctx.Context())
			if err != nil {
				return err
			}
			defer cleanup()

			found, err := engine.Devices(ctx.Context(), ctx.Int("limit"))
			if err != nil {
				return err
			}

			if len(found) == 0 {
				ctx.Println("no devices; run `bsctl scl import <file.scd>`")

				return nil
			}

			for _, ied := range found {
				ctx.Printf("%s  %-16s %-16s %s\n",
					ied.ID, truncate(ied.Name, 16), truncate(ied.Type, 16), ied.Manufacturer)
			}

			return nil
		},
		cli.WithFlag(cli.NewIntFlag("limit", "", "maximum results", 50)),
	)
}

// sclListenersCommand answers the question the electrical model cannot.
func sclListenersCommand() cli.Command {
	return cli.NewCommand(
		"listeners",
		"Show who subscribes to a device, and what it subscribes to",
		func(ctx cli.CommandContext) error {
			if ctx.NArgs() < 1 {
				return errors.New("usage: bsctl scl listeners <device-name>")
			}

			engine, cleanup, err := sclEngine(ctx.Context())
			if err != nil {
				return err
			}
			defer cleanup()

			found, err := engine.Device(ctx.Context(), ctx.Args()[0])
			if err != nil {
				return err
			}

			listeners, err := engine.Listeners(ctx.Context(), found.ID)
			if err != nil {
				return err
			}

			listens, err := engine.Listens(ctx.Context(), found.ID)
			if err != nil {
				return err
			}

			ctx.Printf("%s  %s\n", found.Name, found.ID)

			// Listeners first, because that is the question worth asking: if
			// this device is lost, whose logic stops working. The electrical
			// model cannot answer it at all.
			ctx.Printf("\nif %s is lost, these stop receiving:\n", found.Name)
			printEdges(ctx, listeners, "subscriber")

			ctx.Printf("\n%s listens to:\n", found.Name)
			printEdges(ctx, listens, "publisher")

			return nil
		},
		cli.WithUsage("bsctl scl listeners <device-name>"),
	)
}

// printEdges renders one direction of the subscription graph.
func printEdges(ctx cli.CommandContext, edges []device.Subscription, end string) {
	if len(edges) == 0 {
		ctx.Println("  nothing")

		return
	}

	for _, edge := range edges {
		other := edge.PublisherID
		if end == "subscriber" {
			other = edge.SubscriberID
		}

		ctx.Printf("  %s  %-24s %s\n", other, edge.SubscriberPath, edge.Data)
	}
}

// sclEngine builds an application and resolves the SCL engine from it.
func sclEngine(ctx context.Context) (*scl.Engine, func(), error) {
	app, err := App()
	if err != nil {
		return nil, nil, err
	}

	if err := app.Start(ctx); err != nil {
		return nil, nil, fmt.Errorf("starting the application: %w", err)
	}

	cleanup := func() { _ = app.Stop(context.WithoutCancel(ctx)) }

	engine, err := forge.Inject[*scl.Engine](app.Container())
	if err != nil {
		cleanup()

		return nil, nil, fmt.Errorf("resolving the SCL engine: %w", err)
	}

	return engine, cleanup, nil
}
