package bsctl

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/cli"

	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/weather"
	"github.com/xraph/blackstart/extensions/weather/field"
	wid "github.com/xraph/blackstart/extensions/weather/id"
	"github.com/xraph/blackstart/extensions/weather/model"
	"github.com/xraph/blackstart/extensions/weather/store"
	pkgstore "github.com/xraph/blackstart/pkg/store"
)

// weatherCommand groups the weather subcommands.
func weatherCommand() cli.Command {
	return cli.NewCommand(
		"weather",
		"Retrieve, sample and inspect numerical weather prediction",
		func(ctx cli.CommandContext) error {
			ctx.Println("usage: bsctl weather <ingest|sample|cycles|show>")

			return nil
		},
		cli.WithSubcommand(weatherIngestCommand()),
		cli.WithSubcommand(weatherSampleCommand()),
		cli.WithSubcommand(weatherCyclesCommand()),
		cli.WithSubcommand(weatherShowCommand()),
	)
}

// weatherIngestCommand retrieves one cycle.
func weatherIngestCommand() cli.Command {
	return cli.NewCommand(
		"ingest",
		"Retrieve one model cycle from the public registry",
		func(ctx cli.CommandContext) error {
			engine, cleanup, err := weatherEngine(ctx.Context())
			if err != nil {
				return err
			}

			defer cleanup()

			issuedAt, err := time.Parse(time.RFC3339, ctx.String("cycle"))
			if err != nil {
				return fmt.Errorf(
					"bsctl: --cycle must be an RFC 3339 timestamp: %w", err)
			}

			hours, err := parseHourRange(ctx.String("hours"))
			if err != nil {
				return err
			}

			result, err := engine.Ingest(ctx.Context(), weather.IngestRequest{
				SourceSlug: ctx.String("source"),
				FeedSlug:   ctx.String("feed"),
				Model:      model.Model(ctx.String("model")),
				IssuedAt:   issuedAt,
				Hours:      hours,
			})
			if err != nil {
				return err
			}

			ctx.Printf("cycle     %s\n", result.CycleID)
			ctx.Printf("retrieved %d\n", result.Retrieved)
			ctx.Printf("skipped   %d\n", result.Skipped)
			ctx.Printf("absent    %d\n", result.Absent)
			ctx.Printf("failed    %d\n", result.Failed)

			return nil
		},
		cli.WithFlag(cli.NewStringFlag("source", "", "registry source slug", "noaa")),
		cli.WithFlag(cli.NewStringFlag("feed", "", "feed slug within the source", "hrrr")),
		cli.WithFlag(cli.NewStringFlag("model", "", "model: hrrr or gfs", "hrrr")),
		cli.WithFlag(cli.NewStringFlag("cycle", "", "issuance, RFC 3339", "")),
		cli.WithFlag(cli.NewStringFlag("hours", "", "forecast hours, e.g. 0 or 1-6", "0")),
	)
}

// weatherSampleCommand samples a stored cycle onto spans.
func weatherSampleCommand() cli.Command {
	return cli.NewCommand(
		"sample",
		"Sample a retrieved cycle onto the network's spans",
		func(ctx cli.CommandContext) error {
			engine, cleanup, err := weatherEngine(ctx.Context())
			if err != nil {
				return err
			}

			defer cleanup()

			cycle, err := wid.ParseCycle(ctx.String("cycle"))
			if err != nil {
				return err
			}

			result, err := engine.SampleCycle(ctx.Context(), cycle, nil)
			if err != nil {
				return err
			}

			ctx.Printf("grids   %d\n", result.Grids)
			ctx.Printf("samples %d\n", result.Samples)

			// Covered against total is where a span outside the model's domain
			// shows. HRRR is CONUS-only, so a gap here is information rather
			// than a fault.
			ctx.Printf("covered %d of %d span-readings\n", result.Covered, result.Total)

			return nil
		},
		cli.WithFlag(cli.NewStringFlag("cycle", "", "cycle identifier (wxcyc_…)", "")),
	)
}

// weatherCyclesCommand lists what has been retrieved.
func weatherCyclesCommand() cli.Command {
	return cli.NewCommand(
		"cycles",
		"List retrieved model cycles, newest first",
		func(ctx cli.CommandContext) error {
			engine, cleanup, err := weatherEngine(ctx.Context())
			if err != nil {
				return err
			}

			defer cleanup()

			found, err := engine.Cycles(ctx.Context(),
				store.CycleFilter{Model: model.Model(ctx.String("model"))},
				pkgstore.Page{Limit: ctx.Int("limit")})
			if err != nil {
				return err
			}

			if len(found) == 0 {
				ctx.Println("no cycles retrieved yet")

				return nil
			}

			for _, cycle := range found {
				grids, err := engine.Grids(ctx.Context(), cycle.ID)
				if err != nil {
					return err
				}

				ctx.Printf("%s  %-5s %s  %-8s %d fields\n",
					cycle.ID, cycle.Model,
					cycle.IssuedAt.Format(time.RFC3339),
					cycle.Status, len(grids))
			}

			return nil
		},
		cli.WithFlag(cli.NewStringFlag("model", "", "restrict to one model", "")),
		cli.WithFlag(cli.NewIntFlag("limit", "", "maximum results", 20)),
	)
}

// weatherShowCommand prints a span's forecast.
func weatherShowCommand() cli.Command {
	return cli.NewCommand(
		"show",
		"Show what a run expected at one span",
		func(ctx cli.CommandContext) error {
			engine, cleanup, err := weatherEngine(ctx.Context())
			if err != nil {
				return err
			}

			defer cleanup()

			span, err := gid.ParseSpan(ctx.String("span"))
			if err != nil {
				return err
			}

			issued, err := time.Parse(time.RFC3339, ctx.String("issued"))
			if err != nil {
				return fmt.Errorf(
					"bsctl: --issued must be an RFC 3339 timestamp: %w", err)
			}

			var want []field.Field

			if name := ctx.String("field"); name != "" {
				got := field.Field(name)
				if !got.Valid() {
					return fmt.Errorf("bsctl: %q is not a weather field", name)
				}

				want = []field.Field{got}
			}

			// A generous window rather than a required one: at the command
			// line the useful question is "what did this run say", and a
			// forecast horizon is days at most.
			const horizon = 15 * 24 * time.Hour

			samples, err := engine.SpanForecast(
				ctx.Context(), span, want, issued, issued, issued.Add(horizon))
			if err != nil {
				return err
			}

			if len(samples) == 0 {
				ctx.Println("no samples for that span and issuance")

				return nil
			}

			for _, sample := range samples {
				spec, specErr := field.SpecFor(sample.Field, model.HRRR)

				unit := ""
				if specErr == nil {
					unit = " " + spec.Unit.String()
				}

				ctx.Printf("%s  %-20s %12.4f%s\n",
					sample.ValidAt.Format(time.RFC3339), sample.Field,
					sample.Value, unit)
			}

			return nil
		},
		cli.WithFlag(cli.NewStringFlag("span", "", "span identifier (span_…)", "")),
		cli.WithFlag(cli.NewStringFlag("issued", "", "issuance, RFC 3339", "")),
		cli.WithFlag(cli.NewStringFlag("field", "", "field name; omit for every field", "")),
	)
}

// weatherEngine builds an application and resolves the weather engine from it.
//
// Every subcommand needs one, and none of them needs anything else the
// application provides — so the shape is the same each time and the cleanup
// matters: a CLI invocation that left the fabric worker running would not
// exit.
func weatherEngine(ctx context.Context) (*weather.Engine, func(), error) {
	app, err := App()
	if err != nil {
		return nil, nil, err
	}

	if err := app.Start(ctx); err != nil {
		return nil, nil, fmt.Errorf("starting the application: %w", err)
	}

	cleanup := func() { _ = app.Stop(context.WithoutCancel(ctx)) }

	engine, err := forge.Inject[*weather.Engine](app.Container())
	if err != nil {
		cleanup()

		return nil, nil, fmt.Errorf("resolving the weather engine: %w", err)
	}

	return engine, cleanup, nil
}

// parseHourRange reads "0", "1-6" or "0,3,6" into forecast hours.
//
// Three shapes because all three are natural to type, and a flag that accepted
// only one would have an operator reaching for a shell loop.
func parseHourRange(raw string) ([]int, error) {
	if raw == "" {
		return []int{0}, nil
	}

	var out []int

	for part := range strings.SplitSeq(raw, ",") {
		bounds := strings.Split(part, "-")

		switch len(bounds) {
		case 1:
			hour, err := strconv.Atoi(bounds[0])
			if err != nil {
				return nil, err
			}

			out = append(out, hour)

		case 2:
			low, err := strconv.Atoi(bounds[0])
			if err != nil {
				return nil, err
			}

			high, err := strconv.Atoi(bounds[1])
			if err != nil {
				return nil, err
			}

			if high < low {
				return nil, fmt.Errorf("bsctl: hour range %q counts backwards", part)
			}

			for hour := low; hour <= high; hour++ {
				out = append(out, hour)
			}

		default:
			return nil, fmt.Errorf("bsctl: %q is not an hour or a range", part)
		}
	}

	return out, nil
}
