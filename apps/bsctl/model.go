package bsctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/xraph/forge"
	"github.com/xraph/forge/cli"

	"github.com/xraph/blackstart/extensions/networkmodel"
	"github.com/xraph/blackstart/extensions/networkmodel/canonical"
	nmid "github.com/xraph/blackstart/extensions/networkmodel/id"
	"github.com/xraph/blackstart/extensions/networkmodel/model"
	"github.com/xraph/blackstart/extensions/networkmodel/viewer"
)

// modelCommand groups everything to do with network models.
//
// A subcommand group rather than four top-level verbs, because `bsctl import`
// alone would have to guess what is being imported once there is more than one
// kind of thing to import — and by Phase 2.5 there is.
func modelCommand() cli.Command {
	return cli.NewCommand(
		"model",
		"Import, inspect and export network models",
		func(ctx cli.CommandContext) error {
			ctx.Println("usage: bsctl model <import|list|show|export|place>")

			return nil
		},
		cli.WithSubcommand(modelImportCommand()),
		cli.WithSubcommand(modelListCommand()),
		cli.WithSubcommand(modelShowCommand()),
		cli.WithSubcommand(modelExportCommand()),
		cli.WithSubcommand(modelPlaceCommand()),
	)
}

// modelImportCommand reads a case file and stores the model it describes.
func modelImportCommand() cli.Command {
	return cli.NewCommand(
		"import",
		"Read a MATPOWER case file and store the network it describes",
		func(ctx cli.CommandContext) error {
			if ctx.NArgs() < 1 {
				return errors.New("usage: bsctl model import <case.m>")
			}

			path := ctx.Args()[0]

			source, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("reading %s: %w", path, err)
			}

			engine, cleanup, err := modelEngine(ctx.Context())
			if err != nil {
				return err
			}
			defer cleanup()

			absolute, err := filepath.Abs(path)
			if err != nil {
				absolute = path
			}

			result, err := engine.Import(ctx.Context(), networkmodel.ImportRequest{
				Source: source,
				// A file URL rather than a bare path, so a provenance record
				// says what kind of location it names. A path with no scheme
				// reads as a URL that lost its scheme.
				URL:  "file://" + absolute,
				Name: ctx.String("name"),
			})
			if err != nil {
				return err
			}

			if result.AlreadyPresent {
				ctx.Printf("already known: %s\n", result.Model.ID)
				ctx.Printf("  content hash %s\n", result.Model.ContentHash)
				ctx.Println("  nothing was built; this network was already stored")

				return nil
			}

			ctx.Printf("imported %s\n", result.Model.ID)
			ctx.Printf("  content hash %s\n", result.Model.ContentHash)
			ctx.Printf("  %d buses, %d branches\n",
				result.Model.BusCount, result.Model.BranchCount)

			// Said plainly rather than left to be inferred from a blank field.
			// A model whose chain was not recorded is usable and its numbers
			// are not reproducible from the record alone, and the operator
			// should learn that here rather than from a provenance query weeks
			// later.
			if !result.Traceable {
				ctx.Warning(
					"provenance was not recorded, so this model cannot be traced " +
						"back to the file it came from; results computed from it " +
						"will say so")
			}

			return nil
		},
		cli.WithUsage("bsctl model import <case.m> [--name NAME]"),
		cli.WithFlag(cli.NewStringFlag(
			"name", "", "what to call the model; defaults to the case's own name", "")),
	)
}

// modelListCommand shows what has been imported.
func modelListCommand() cli.Command {
	return cli.NewCommand(
		"list",
		"List stored network models, most recent first",
		func(ctx cli.CommandContext) error {
			engine, cleanup, err := modelEngine(ctx.Context())
			if err != nil {
				return err
			}
			defer cleanup()

			found, err := engine.List(ctx.Context(), ctx.Int("limit"))
			if err != nil {
				return err
			}

			if len(found) == 0 {
				ctx.Println("no models stored; run `bsctl model import <case.m>`")

				return nil
			}

			for _, m := range found {
				ctx.Printf("%s  %-20s %4d buses %4d branches  %s\n",
					m.ID, truncate(m.Name, 20), m.BusCount, m.BranchCount,
					m.ContentHash[:12])
			}

			return nil
		},
		cli.WithFlag(cli.NewIntFlag("limit", "", "maximum results", 20)),
	)
}

// modelShowCommand prints one model.
func modelShowCommand() cli.Command {
	return cli.NewCommand(
		"show",
		"Show one network model",
		func(ctx cli.CommandContext) error {
			if ctx.NArgs() < 1 {
				return errors.New("usage: bsctl model show <nmdl_… | content-hash>")
			}

			engine, cleanup, err := modelEngine(ctx.Context())
			if err != nil {
				return err
			}
			defer cleanup()

			found, err := resolveModel(ctx, engine, ctx.Args()[0])
			if err != nil {
				return err
			}

			ctx.Printf("id            %s\n", found.ID)
			ctx.Printf("name          %s\n", found.Name)
			ctx.Printf("content hash  %s\n", found.ContentHash)
			ctx.Printf("source        %s\n", found.SourceKind)
			ctx.Printf("size          %d buses, %d branches\n",
				found.BusCount, found.BranchCount)
			ctx.Printf("built         %s\n", found.BuiltAt.Format("2006-01-02 15:04:05 MST"))

			if found.DatasetVersionID.IsZero() {
				ctx.Printf("provenance    none recorded\n")
			} else {
				ctx.Printf("provenance    %s\n", found.DatasetVersionID)
			}

			return nil
		},
		cli.WithUsage("bsctl model show <nmdl_… | content-hash>"),
	)
}

// modelExportCommand writes a model out.
func modelExportCommand() cli.Command {
	return cli.NewCommand(
		"export",
		"Write a model's canonical encoding or record to a file",
		func(ctx cli.CommandContext) error {
			if ctx.NArgs() < 1 {
				return errors.New("usage: bsctl model export <nmdl_… | content-hash> [--format]")
			}

			engine, cleanup, err := modelEngine(ctx.Context())
			if err != nil {
				return err
			}
			defer cleanup()

			found, err := resolveModel(ctx, engine, ctx.Args()[0])
			if err != nil {
				return err
			}

			out := ctx.String("out")
			if out == "" {
				// A file rather than stdout, and required rather than
				// defaulted. The framework's logger writes to stdout, so
				// `bsctl model export … > model.bin` produces a file with log
				// lines interleaved through the binary — which decodes as
				// garbage and looks like an encoder bug. Refusing up front
				// beats producing a corrupt artifact.
				return errors.New(
					"--out is required: stdout carries the framework's log " +
						"output, so a redirect would interleave log lines through " +
						"the payload")
			}

			var payload []byte

			switch format := ctx.String("format"); format {
			case "", "canonical":
				// The stored bytes, not a re-encoding. What a result was
				// computed from must be what comes out, and re-encoding would
				// be correct today and diverge the first time the encoder's
				// version moved.
				encoded, err := engine.Encoding(ctx.Context(), found.ContentHash)
				if err != nil {
					return err
				}

				payload = encoded

			case "viewer":
				// Decoded from the stored bytes rather than re-parsed from a
				// source file that may be long gone — and which would anyway
				// risk the export describing a different network from the one
				// the hash names.
				encoded, err := engine.Encoding(ctx.Context(), found.ContentHash)
				if err != nil {
					return err
				}

				decoded, err := canonical.Decode(encoded)
				if err != nil {
					return err
				}

				var placements viewer.Placement

				if coords := ctx.String("coords"); coords != "" {
					if placements, err = readPlacements(coords); err != nil {
						return err
					}
				}

				built, err := viewer.Build(
					found.ID.String(), found.ContentHash, decoded, placements)
				if err != nil {
					return err
				}

				if payload, err = json.Marshal(built); err != nil {
					return err
				}

				// Said plainly. A bus with no coordinate draws at the origin,
				// which is conspicuous in the viewer but reads as a rendering
				// fault rather than as missing data.
				if missing := countMissing(built); missing > 0 {
					ctx.Warning(fmt.Sprintf(
						"%d of %d buses have no coordinate and will draw at "+
							"0°N 0°E; supply --coords bus,lat,lon",
						missing, len(built.Buses.IDs)))
				}

			case "json":
				encoded, err := json.MarshalIndent(found, "", "  ")
				if err != nil {
					return err
				}

				// A trailing newline, so the file plays nicely with every
				// line-oriented tool that will read it.
				payload = append(encoded, '\n') //nolint:gocritic // encoded is not reused

			default:
				return fmt.Errorf(
					"unknown format %q; canonical writes the bytes the solver "+
						"reads, json writes the model record", format)
			}

			if err := os.WriteFile(out, payload, 0o600); err != nil {
				return fmt.Errorf("writing %s: %w", out, err)
			}

			ctx.Printf("wrote %d bytes to %s\n", len(payload), out)

			return nil
		},
		cli.WithUsage(
			"bsctl model export <nmdl_… | content-hash> --out FILE "+
				"[--format canonical|json|viewer] [--coords FILE]"),
		cli.WithFlag(cli.NewStringFlag("out", "o", "file to write", "")),
		cli.WithFlag(cli.NewStringFlag(
			"format", "", "canonical, json or viewer", "canonical")),
		cli.WithFlag(cli.NewStringFlag(
			"coords", "", "CSV of bus,lat,lon[,elevation] for the viewer format", "")),
	)
}

// modelPlaceCommand stores a model's bus coordinates.
//
// A stored coordinate file rather than something passed to one export, so
// that placing a model's buses is a fact recorded once rather than an
// argument every future export or trace has to remember to repeat. The CSV
// stays the input format — it is what the ACTIVSg distribution ships — but
// once it is read here it becomes rows a branch's endpoints can be looked up
// by, which is what `bsctl geo trace-model` needs.
func modelPlaceCommand() cli.Command {
	return cli.NewCommand(
		"place",
		"Store a model's bus coordinates from a CSV",
		func(ctx cli.CommandContext) error {
			if ctx.NArgs() < 1 {
				return errors.New(
					"usage: bsctl model place <nmdl_… | content-hash> --coords FILE")
			}

			engine, cleanup, err := modelEngine(ctx.Context())
			if err != nil {
				return err
			}
			defer cleanup()

			found, err := resolveModel(ctx, engine, ctx.Args()[0])
			if err != nil {
				return err
			}

			coords := ctx.String("coords")
			if coords == "" {
				// Required rather than defaulted, for the same reason --out is
				// required on export: a command with nothing useful to do
				// absent a flag should refuse rather than silently write zero
				// placements.
				return errors.New(
					"--coords is required: a CSV of bus,lat,lon[,elevation]")
			}

			read, err := readPlacements(coords)
			if err != nil {
				return err
			}

			placements := placementsOf(read, found.ID)

			// Resolved against the model's own bus IDs rather than trusted at
			// face value: placement keys must be canonical bus-<n>, but the
			// ACTIVSg distribution ships raw bus numbers, and a CSV in that
			// convention would otherwise report "placed N buses" while every
			// row silently fails to match a branch's endpoint.
			modelBuses, err := busIDs(ctx.Context(), engine, found)
			if err != nil {
				return err
			}

			unplaced, unknown := placementCounts(placements, modelBuses)

			if err := engine.Store().Placements().PutAll(
				ctx.Context(), found.ID, placements); err != nil {
				return err
			}

			// Every number, every time — the same reason `bsctl hazard assume`
			// reports what it could not do, not just what it did. A command
			// that only prints success is how somebody comes to believe a
			// model is placed when it is not.
			ctx.Printf("placed                    %d\n", len(placements))
			ctx.Printf("model buses left unplaced %d\n", unplaced)
			ctx.Printf("csv rows unknown to model  %d\n", unknown)

			return nil
		},
		cli.WithUsage(
			"bsctl model place <nmdl_… | content-hash> --coords FILE"),
		cli.WithFlag(cli.NewStringFlag(
			"coords", "", "CSV of bus,lat,lon[,elevation]", "")),
	)
}

// resolveModel accepts either an identifier or a content hash.
//
// Both because they name the same thing from two directions: an identifier
// comes from a previous command's output, a hash comes from a result someone
// is trying to explain. Making them separate flags would mean the operator has
// to know which kind of string they are holding.
func resolveModel(
	ctx cli.CommandContext, engine *networkmodel.Engine, key string,
) (model.NetworkModel, error) {
	if parsed, err := nmid.ParseModel(key); err == nil {
		return engine.Get(ctx.Context(), parsed)
	}

	return engine.FindByHash(ctx.Context(), key)
}

// modelEngine builds an application and resolves the model engine from it.
//
// Returns a cleanup rather than leaving the application running, because a CLI
// invocation that leaks a connection pool is a CLI invocation that hangs on
// exit.
func modelEngine(ctx context.Context) (*networkmodel.Engine, func(), error) {
	app, err := App()
	if err != nil {
		return nil, nil, err
	}

	if err := app.Start(ctx); err != nil {
		return nil, nil, fmt.Errorf("starting the application: %w", err)
	}

	// Stop gets a fresh context rather than the command's, so a canceled
	// command still shuts its connections down instead of leaking them on the
	// way out.
	cleanup := func() { _ = app.Stop(context.WithoutCancel(ctx)) }

	engine, err := forge.Inject[*networkmodel.Engine](app.Container())
	if err != nil {
		cleanup()

		return nil, nil, fmt.Errorf("resolving the model engine: %w", err)
	}

	return engine, cleanup, nil
}

// truncate shortens a name for a fixed-width listing.
func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}

	return s[:limit-1] + "…"
}

// countMissing counts buses that have no coordinate.
//
// Exactly at the origin, which is the sentinel a missing placement leaves. A
// real bus at 0°N 0°E would be in the Gulf of Guinea, and there are no
// substations there.
func countMissing(payload viewer.Model) int {
	var missing int

	for index := range payload.Buses.IDs {
		if payload.Buses.Coordinates[index*3] == 0 &&
			payload.Buses.Coordinates[index*3+1] == 0 {
			missing++
		}
	}

	return missing
}

// busIDs is the canonical bus-<n> identifier of every bus in a stored
// model, resolved from its own stored encoding.
//
// A count (NetworkModel.BusCount) says how many buses a model has, not
// which identifiers name them, and modelPlaceCommand needs the latter to
// tell a CSV row naming a real bus from one naming a bus number the ACTIVSg
// distribution ships but the canonical model does not recognize.
func busIDs(
	ctx context.Context, engine *networkmodel.Engine, found model.NetworkModel,
) (map[string]bool, error) {
	encoded, err := engine.Encoding(ctx, found.ContentHash)
	if err != nil {
		return nil, err
	}

	decoded, err := canonical.Decode(encoded)
	if err != nil {
		return nil, err
	}

	out := make(map[string]bool, len(decoded.Buses))
	for _, bus := range decoded.Buses {
		out[bus.ID] = true
	}

	return out, nil
}
