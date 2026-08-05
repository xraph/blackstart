package bsctl

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/xraph/forge"
	"github.com/xraph/forge/cli"

	"github.com/xraph/blackstart/extensions/geo"
	"github.com/xraph/blackstart/extensions/geo/asset"
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
			ctx.Println("usage: bsctl geo <trace|import|show>")

			return nil
		},
		cli.WithSubcommand(geoTraceCommand()),
		cli.WithSubcommand(geoImportCommand()),
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
			ctx.Printf("  branch      %s\n", route.CarrierID)
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

			structures, err := engine.Structures(ctx.Context(), route.ID)
			if err != nil {
				return err
			}

			ctx.Printf("id          %s\n", route.ID)
			ctx.Printf("carrier     %s\n", route.CarrierID)
			ctx.Printf("provenance  %s\n", route.Provenance)
			ctx.Printf("length      %.1f km in %d spans\n", route.LengthM/1000, len(spans))

			// The first and last few rather than all of them: a 100 km line has
			// hundreds, and printing them all buries the two that say whether
			// the route starts and ends where it should.
			for _, span := range headAndTail(spans, 3) {
				ctx.Printf("  span %-4d %8.1f m  bearing %5.1f°  %v\n",
					span.Sequence, span.LengthM, span.AzimuthDeg, span.Geometry[0])
			}

			printStructures(ctx, structures)

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

// printStructures reports a route's structure count, class distribution, and
// its first and last structure.
//
// Count and class rather than a full listing, for headAndTail's reason: a
// long line has hundreds of structures, and the two that matter for eyeballing
// whether the geometry landed the right way round — and whether classes
// survived the import — are the first and last.
func printStructures(ctx cli.CommandContext, structures []asset.Structure) {
	ctx.Printf("structures  %d  (%s)\n", len(structures), classDistribution(structures))

	if len(structures) == 0 {
		return
	}

	first := structures[0]
	ctx.Printf("  %-6s %s  %.4f, %.4f  %s\n",
		"first", first.ID, first.Location.Lat, first.Location.Lon, structureClass(first))

	if len(structures) > 1 {
		last := structures[len(structures)-1]
		ctx.Printf("  %-6s %s  %.4f, %.4f  %s\n",
			"last", last.ID, last.Location.Lat, last.Location.Lon, structureClass(last))
	}
}

// classDistribution summarizes how many structures fall into each Kind.
//
// Unclassed is counted rather than merged into any class, and always sorts
// last regardless of its count. It is exactly the set of structures the risk
// extension will be unable to fit a fragility curve to, and surfacing it here
// — at import time — is cheaper than discovering it as a silent gap in a risk
// computation.
func classDistribution(structures []asset.Structure) string {
	counts := make(map[string]int, len(structures))
	for _, s := range structures {
		counts[structureKind(s)]++
	}

	classes := make([]string, 0, len(counts))
	for kind := range counts {
		classes = append(classes, kind)
	}

	sort.Slice(classes, func(i, j int) bool {
		if classes[i] == unclassed {
			return false
		}

		if classes[j] == unclassed {
			return true
		}

		if counts[classes[i]] != counts[classes[j]] {
			return counts[classes[i]] > counts[classes[j]]
		}

		return classes[i] < classes[j]
	})

	parts := make([]string, 0, len(classes))
	for _, kind := range classes {
		parts = append(parts, fmt.Sprintf("%s %d", kind, counts[kind]))
	}

	return strings.Join(parts, ", ")
}

// unclassed labels a structure whose source recorded no Kind.
const unclassed = "unclassed"

// structureKind is a structure's Kind, or unclassed when the source recorded
// none.
func structureKind(s asset.Structure) string {
	if s.Kind == "" {
		return unclassed
	}

	return s.Kind
}

// structureClass renders a structure's kind and, when known, its material.
func structureClass(s asset.Structure) string {
	kind := structureKind(s)
	if s.Material == "" {
		return kind
	}

	return kind + "/" + s.Material
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
