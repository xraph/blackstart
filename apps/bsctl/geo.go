package bsctl

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/xraph/forge"
	"github.com/xraph/forge/cli"

	"github.com/xraph/blackstart/extensions/geo"
	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/geo/shape"
	"github.com/xraph/blackstart/pkg/id"
)

// geoCommand groups everything to do with where the network is.
func geoCommand() cli.Command {
	return cli.NewCommand(
		"geo",
		"Trace and inspect where the network physically is",
		func(ctx cli.CommandContext) error {
			ctx.Println("usage: bsctl geo <trace|show>")

			return nil
		},
		cli.WithSubcommand(geoTraceCommand()),
		cli.WithSubcommand(geoShowCommand()),
	)
}

// geoTraceCommand builds a synthetic route between two coordinates.
func geoTraceCommand() cli.Command {
	return cli.NewCommand(
		"trace",
		"Build a synthetic route for a branch between two coordinates",
		func(ctx cli.CommandContext) error {
			if ctx.NArgs() < 2 {
				return errors.New(
					"usage: bsctl geo trace --branch ID <lat,lon> <lat,lon>")
			}

			branch, err := id.Parse(ctx.String("branch"))
			if err != nil {
				return fmt.Errorf("--branch: %w", err)
			}

			from, err := parseCoordinate(ctx.Args()[0])
			if err != nil {
				return err
			}

			to, err := parseCoordinate(ctx.Args()[1])
			if err != nil {
				return err
			}

			engine, cleanup, err := geoEngine(ctx.Context())
			if err != nil {
				return err
			}
			defer cleanup()

			route, err := engine.Trace(ctx.Context(), geo.TraceRequest{
				BranchID: branch,
				From:     from,
				To:       to,
				SpanM:    float64(ctx.Int("span")),
			})
			if err != nil {
				return err
			}

			spans, err := engine.Spans(ctx.Context(), route.ID)
			if err != nil {
				return err
			}

			ctx.Printf("traced %s\n", route.ID)
			ctx.Printf("  branch      %s\n", route.BranchID)
			ctx.Printf("  provenance  %s\n", route.Provenance)
			ctx.Printf("  length      %.1f km in %d spans\n", route.LengthM/1000, len(spans))

			// Said at every trace, not buried in a field. A per-span failure
			// probability conditioned on a straight-line guess is a different
			// claim from one conditioned on a surveyed route, and the operator
			// should know which one they just created.
			ctx.Warning(
				"this route is interpolated: a straight great-circle path with " +
					"evenly spaced imaginary structures, not a surveyed " +
					"right-of-way")

			return nil
		},
		cli.WithUsage(
			"bsctl geo trace --branch ID <lat,lon> <lat,lon> [--span METERS]"),
		cli.WithFlag(cli.NewStringFlag("branch", "", "branch this route carries", "")),
		cli.WithFlag(cli.NewIntFlag(
			"span", "", "nominal meters between structures", geo.DefaultSpanM)),
	)
}

// geoShowCommand prints a route and the ends of its spans.
func geoShowCommand() cli.Command {
	return cli.NewCommand(
		"show",
		"Show a route and its spans",
		func(ctx cli.CommandContext) error {
			if ctx.NArgs() < 1 {
				return errors.New("usage: bsctl geo show <rte_…>")
			}

			want, err := gid.ParseRoute(ctx.Args()[0])
			if err != nil {
				return err
			}

			engine, cleanup, err := geoEngine(ctx.Context())
			if err != nil {
				return err
			}
			defer cleanup()

			route, err := engine.Route(ctx.Context(), want)
			if err != nil {
				return err
			}

			spans, err := engine.Spans(ctx.Context(), route.ID)
			if err != nil {
				return err
			}

			ctx.Printf("id          %s\n", route.ID)
			ctx.Printf("branch      %s\n", route.BranchID)
			ctx.Printf("provenance  %s\n", route.Provenance)
			ctx.Printf("length      %.1f km in %d spans\n", route.LengthM/1000, len(spans))

			// The first and last few rather than all of them: a 100 km line has
			// hundreds, and printing them all buries the two that say whether
			// the route starts and ends where it should.
			for _, span := range headAndTail(spans, 3) {
				ctx.Printf("  span %-4d %8.1f m  bearing %5.1f°  %v\n",
					span.Sequence, span.LengthM, span.AzimuthDeg, span.Geometry[0])
			}

			return nil
		},
		cli.WithUsage("bsctl geo show <rte_…>"),
	)
}

// geoEngine builds an application and resolves the geo engine from it.
func geoEngine(ctx context.Context) (*geo.Engine, func(), error) {
	app, err := App()
	if err != nil {
		return nil, nil, err
	}

	if err := app.Start(ctx); err != nil {
		return nil, nil, fmt.Errorf("starting the application: %w", err)
	}

	cleanup := func() { _ = app.Stop(context.WithoutCancel(ctx)) }

	engine, err := forge.Inject[*geo.Engine](app.Container())
	if err != nil {
		cleanup()

		return nil, nil, fmt.Errorf("resolving the geo engine: %w", err)
	}

	return engine, cleanup, nil
}

// headAndTail returns the first and last n items, or everything when there are
// fewer than twice n.
//
// A 100 km line has hundreds of spans, and printing them all buries the two
// that say whether the route starts and ends where it should.
func headAndTail[T any](items []T, n int) []T {
	if len(items) <= 2*n {
		return items
	}

	out := make([]T, 0, 2*n)
	out = append(out, items[:n]...)
	out = append(out, items[len(items)-n:]...)

	return out
}

// parseCoordinate reads a "lat,lon" pair.
//
// Latitude first, which is the order every mapping site and every human uses,
// and the opposite of the (lon, lat) order the geometry itself is stored in.
// The flip happens here, once, at the boundary — which is the only place it
// can be got wrong exactly once instead of everywhere.
func parseCoordinate(text string) (shape.Point, error) {
	latText, lonText, ok := strings.Cut(text, ",")
	if !ok {
		return shape.Point{}, fmt.Errorf(
			"%q is not a coordinate; write it as lat,lon — for example "+
				"32.2983,-97.7856", text)
	}

	lat, err := strconv.ParseFloat(strings.TrimSpace(latText), 64)
	if err != nil {
		return shape.Point{}, fmt.Errorf("latitude %q is not a number", latText)
	}

	lon, err := strconv.ParseFloat(strings.TrimSpace(lonText), 64)
	if err != nil {
		return shape.Point{}, fmt.Errorf("longitude %q is not a number", lonText)
	}

	out := shape.Point{Lon: lon, Lat: lat}

	return out, out.Validate()
}
