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
	"github.com/xraph/blackstart/extensions/networkmodel"
	"github.com/xraph/blackstart/extensions/networkmodel/canonical"
	"github.com/xraph/blackstart/extensions/networkmodel/model"
	"github.com/xraph/blackstart/pkg/bserr"
	"github.com/xraph/blackstart/pkg/id"
)

// geoCommand groups everything to do with where the network is.
func geoCommand() cli.Command {
	return cli.NewCommand(
		"geo",
		"Trace and inspect where the network physically is",
		func(ctx cli.CommandContext) error {
			ctx.Println("usage: bsctl geo <trace|trace-model|import|show>")

			return nil
		},
		cli.WithSubcommand(geoTraceCommand()),
		cli.WithSubcommand(geoTraceModelCommand()),
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

// geoTraceModelCommand traces a route for every branch of a model that has
// placements at both ends.
//
// This is what `bsctl geo trace` could not do: trace a whole network's
// branches at once rather than one hand-typed coordinate pair at a time,
// which is why only four branch routes existed on a 2000-bus case.
func geoTraceModelCommand() cli.Command {
	return cli.NewCommand(
		"trace-model",
		"Trace a route for every branch of a model that has placements at both ends",
		func(ctx cli.CommandContext) error {
			if ctx.NArgs() < 1 {
				return errors.New(
					"usage: bsctl geo trace-model <nmdl_… | content-hash> " +
						"[--dry-run] [--span METERS]")
			}

			nmEngine, gxEngine, cleanup, err := modelAndGeo(ctx.Context())
			if err != nil {
				return err
			}
			defer cleanup()

			found, err := resolveModel(ctx, nmEngine, ctx.Args()[0])
			if err != nil {
				return err
			}

			candidates, noPlacement, alreadyTraced, transformers, err := traceCandidates(
				ctx.Context(), nmEngine, gxEngine, found.ID)
			if err != nil {
				return err
			}

			dryRun := ctx.Bool("dry-run")
			spanM := float64(ctx.Int("span"))
			traced := 0

			for _, candidate := range candidates {
				if !dryRun {
					if _, err := gxEngine.Trace(ctx.Context(), geo.TraceRequest{
						BranchID: candidate.Branch.ID,
						From:     candidate.From,
						To:       candidate.To,
						SpanM:    spanM,
					}); err != nil {
						return fmt.Errorf(
							"bsctl: tracing branch %s: %w", candidate.Branch.ID, err)
					}
				}

				traced++
			}

			if dryRun {
				ctx.Println("dry run — nothing was written")
			}

			// Every number, every time — the same reason `bsctl hazard assume`
			// prints all of its counts. "No placement" is a gap in the
			// coordinate file; "already traced" is a previous run having done
			// its job, and running this command twice must not double the
			// routes; "transformers" is a branch this command correctly
			// refuses to trace a corridor for.
			ctx.Printf("\ntraced            %d\n", traced)
			ctx.Printf("transformers      %d\n", transformers)
			ctx.Printf("already traced    %d\n", alreadyTraced)
			ctx.Printf("no placement      %d\n", noPlacement)
			ctx.Printf("branches total    %d\n", traced+transformers+alreadyTraced+noPlacement)

			return nil
		},
		cli.WithUsage(
			"bsctl geo trace-model <nmdl_… | content-hash> [--dry-run] [--span METERS]"),
		cli.WithFlag(cli.NewBoolFlag(
			"dry-run", "", "report what would be traced without writing", false)),
		cli.WithFlag(cli.NewIntFlag(
			"span", "", "nominal meters between structures", geo.DefaultSpanM)),
	)
}

// traceCandidate is one branch ready to be traced: the branch itself and the
// two placements its endpoints resolved to.
type traceCandidate struct {
	Branch   model.Branch
	From, To shape.Point
}

// traceCandidates walks a model's branches and sorts each into one of four
// piles: a transformer, already traced, missing a placement at one or both
// ends, or ready to trace.
//
// Checked in that order, and the order is not arbitrary:
//
//   - A transformer is decided first because it is the most specific
//     statement that can be made about a branch: a transformer is never
//     traceable whatever else is true of it, so nothing else needs deciding
//     once its two ends state different voltages — not whether it already
//     has a route, not whether it has placements at all. This is the
//     identical rule extensions/hazard/conductor.PlanAssumptions applies to
//     keep from inventing a conductor for a transformer, read from the same
//     model.Bus rows, so geo and hazard land on the same branches by
//     construction rather than by coincidence.
//   - Already traced comes next because it is a fact about the world: a
//     route exists. That fact must win over "no placement" or this command
//     would stop being idempotent — a placement removed after a route was
//     traced must not make the route reappear as untraced.
//   - No placement is last because it is the one gap of the three somebody
//     could still close by adding a coordinate. A transformer never becomes
//     traceable and an existing route is not undone by this command, but a
//     missing placement is exactly the kind of absence a later import might
//     fill.
//
// A branch missing either placement is counted and skipped rather than given
// an invented coordinate — a model with no placements, or an incomplete one,
// is a legitimate state, and everything downstream reports its absence as a
// count rather than a guess.
//
// The transformer check only catches a branch whose two ends state different
// voltages. It does not catch a 1:1 transformer or a same-voltage phase
// shifter — both state one voltage at both ends and pass this check exactly
// like an ordinary conductor run. canonical.Branch carries Tap and Shift,
// which would catch them, but the relational model.Branch row this function
// reads does not project those fields, and adding them would mean a
// migration, an import-path change, and a re-import of every model —
// deliberately out of scope here. This gap is not closed by this change; it
// is only the gap that remains once the accidental one is.
func traceCandidates(
	ctx context.Context, nm *networkmodel.Engine, gx *geo.Engine, modelID id.ID,
) (candidates []traceCandidate, noPlacement, alreadyTraced, transformers int, err error) {
	branches, err := nm.Store().Branches().ForModel(ctx, modelID)
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("bsctl: reading branches of %s: %w", modelID, err)
	}

	placements, err := nm.Store().Placements().ForModel(ctx, modelID)
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("bsctl: reading placements of %s: %w", modelID, err)
	}

	buses, err := nm.Store().Buses().ForModel(ctx, modelID)
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("bsctl: reading buses of %s: %w", modelID, err)
	}

	byBus := make(map[string]model.Placement, len(placements))
	for _, placement := range placements {
		byBus[placement.Bus] = placement
	}

	busByOrdinal := make(map[string]model.Bus, len(buses))
	for _, bus := range buses {
		busByOrdinal[bus.Ordinal] = bus
	}

	for _, branch := range branches {
		// A bus absent from the map — a model with no bus rows at all, or one
		// missing just this end — zero-values to a Bus whose VNominalV is 0,
		// which canonical.StatedNominalV reads as unstated. That is exactly
		// the reading this check needs: an absent bus row must never be
		// mistaken for a stated voltage of zero.
		if branchIsTransformer(busByOrdinal[branch.FromBus], busByOrdinal[branch.ToBus]) {
			transformers++

			continue
		}

		// Idempotent by construction: a branch already carrying a route is
		// counted and skipped, so running this command twice does not double
		// the routes.
		_, err := gx.RouteForBranch(ctx, branch.ID)
		if err == nil {
			alreadyTraced++

			continue
		}

		if !errors.Is(err, bserr.ErrNotFound) {
			return nil, 0, 0, 0, fmt.Errorf(
				"bsctl: checking whether %s is already traced: %w", branch.ID, err)
		}

		from, ok := byBus[branch.FromBus]
		if !ok {
			noPlacement++

			continue
		}

		to, ok := byBus[branch.ToBus]
		if !ok {
			noPlacement++

			continue
		}

		candidates = append(candidates, traceCandidate{
			Branch: branch,
			From:   shape.Point{Lon: from.Lon, Lat: from.Lat, Elevation: from.Elevation},
			To:     shape.Point{Lon: to.Lon, Lat: to.Lat, Elevation: to.Elevation},
		})
	}

	return candidates, noPlacement, alreadyTraced, transformers, nil
}

// branchIsTransformer reports whether a branch's two ends state different
// nominal voltages.
//
// This is the same rule extensions/hazard/conductor.PlanAssumptions applies
// to bucket transformers out of its own candidates, reproduced here rather
// than shared by import so geo does not have to depend on hazard to agree
// with it: both read canonical.StatedNominalV off the same model.Bus rows,
// so the two commands land on the same branches by construction.
//
// Both ends must state a voltage before this returns true. One end silent is
// not evidence of a transformer — absence must never imply a conclusion —
// so a branch with only one stated end is left to trace normally, exactly as
// PlanAssumptions leaves it to be assumed by voltage rather than flagged as a
// transformer. "States a voltage" is canonical.StatedNominalV and is not
// re-spelled here as `volts > 0`: that function exists because this question
// had already been answered three different ways once, and a bus holding
// +Inf read as stated in two of them.
func branchIsTransformer(from, to model.Bus) bool {
	fromVolts, fromStated := canonical.StatedNominalV(from.VNominalV)
	toVolts, toStated := canonical.StatedNominalV(to.VNominalV)

	return fromStated && toStated && fromVolts != toVolts
}

// modelAndGeo resolves both the network-model and geo engines from one
// application.
//
// One application, not two. Each helper starts its own, and starting the
// stack twice in one command opens two connection pools and runs the
// migration orchestrator concurrently against the same database — the same
// reasoning as hazardGeoAndModel, for the same one-application pattern.
func modelAndGeo(
	ctx context.Context,
) (*networkmodel.Engine, *geo.Engine, func(), error) {
	app, err := App()
	if err != nil {
		return nil, nil, nil, err
	}

	if err := app.Start(ctx); err != nil {
		return nil, nil, nil, fmt.Errorf("starting the application: %w", err)
	}

	cleanup := func() { _ = app.Stop(context.WithoutCancel(ctx)) }

	nm, err := forge.Inject[*networkmodel.Engine](app.Container())
	if err != nil {
		cleanup()

		return nil, nil, nil, fmt.Errorf("resolving the model engine: %w", err)
	}

	gx, err := forge.Inject[*geo.Engine](app.Container())
	if err != nil {
		cleanup()

		return nil, nil, nil, fmt.Errorf("resolving the geo engine: %w", err)
	}

	return nm, gx, cleanup, nil
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
