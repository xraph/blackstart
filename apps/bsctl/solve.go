package bsctl

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/forge/cli"

	"github.com/xraph/blackstart/extensions/contingency"
	"github.com/xraph/blackstart/extensions/contingency/sweep"
	"github.com/xraph/blackstart/extensions/networkmodel"
	"github.com/xraph/blackstart/extensions/networkmodel/canonical"
	"github.com/xraph/blackstart/extensions/networkmodel/wire"
	"github.com/xraph/blackstart/extensions/powerflow"
	"github.com/xraph/blackstart/extensions/powerflow/solution"
	solverv1 "github.com/xraph/blackstart/proto/gen/go/blackstart/solver/v1"
)

// solveCommand runs a power flow against a stored model.
func solveCommand() cli.Command {
	return cli.NewCommand(
		"solve",
		"Run a power flow against a stored network model",
		func(ctx cli.CommandContext) error {
			if ctx.NArgs() < 1 {
				return errors.New("usage: bsctl solve <nmdl_… | content-hash> [--method ac|dc]")
			}

			app, cleanup, err := startApp(ctx.Context())
			if err != nil {
				return err
			}
			defer cleanup()

			model, err := wireModel(ctx, app)
			if err != nil {
				return err
			}

			engine, err := forge.Inject[*powerflow.Engine](app.Container())
			if err != nil {
				return fmt.Errorf(
					"resolving the power-flow engine: %w; it is registered only "+
						"when a solver address is configured", err)
			}

			method := solution.Method(ctx.String("method"))
			if method == "" {
				method = solution.MethodAC
			}

			result, err := engine.Solve(ctx.Context(), powerflow.Request{
				Model:                 model,
				Method:                method,
				EnforceReactiveLimits: ctx.Bool("limits"),
			})
			if err != nil {
				return err
			}

			ctx.Printf("solved %s\n", result.ID)
			ctx.Printf("  method        %s\n", result.Method)
			ctx.Printf("  model         %s\n", result.ModelHash[:16])
			ctx.Printf("  solver        %s\n", result.SolverVersion)
			ctx.Printf("  iterations    %d\n", result.Iterations)
			ctx.Printf("  residual      %.3e\n", result.Residual)
			ctx.Printf("  reactive lim. %v\n", result.EnforcedReactiveLimits)

			// The most loaded branch, because that is the number anyone reads
			// a power flow for. Printing every branch of a 2000-bus network
			// buries it.
			if worst, loading := worstBranch(result); worst != "" {
				ctx.Printf("  worst loading %s at %.1f%%\n", worst, loading*100)
			}

			if len(result.BindingGenerators) > 0 {
				ctx.Printf("  %d generator(s) at a reactive limit\n",
					len(result.BindingGenerators))
			}

			return nil
		},
		cli.WithUsage("bsctl solve <nmdl_… | content-hash> [--method ac|dc] [--limits]"),
		cli.WithFlag(cli.NewStringFlag("method", "m", "ac or dc", "ac")),
		cli.WithFlag(cli.NewBoolFlag(
			"limits", "", "enforce generator reactive limits", false)),
	)
}

// n1Command runs a contingency sweep.
func n1Command() cli.Command {
	return cli.NewCommand(
		"n1",
		"Screen every single-branch outage and report what it overloads",
		func(ctx cli.CommandContext) error {
			if ctx.NArgs() < 1 {
				return errors.New("usage: bsctl n1 <nmdl_… | content-hash>")
			}

			app, cleanup, err := startApp(ctx.Context())
			if err != nil {
				return err
			}
			defer cleanup()

			model, err := wireModel(ctx, app)
			if err != nil {
				return err
			}

			engine, err := forge.Inject[*contingency.Engine](app.Container())
			if err != nil {
				return fmt.Errorf(
					"resolving the contingency engine: %w; it is registered only "+
						"when a solver address is configured", err)
			}

			run, err := engine.Screen(ctx.Context(), contingency.Request{
				Model: model,
				// Zero means the rating itself, which the engine documents, so
				// an unset flag needs no default here.
				ViolationThreshold: float64(ctx.Int("threshold-percent")) / 100,
			})
			if err != nil {
				return err
			}

			ctx.Printf("screened %s (threshold %.2f)\n", run.ID, float64(ctx.Int("threshold-percent"))/100)
			ctx.Printf("  model      %s\n", run.ModelHash[:16])
			ctx.Printf("  evaluated  %d contingencies\n", len(run.Candidates))

			report(ctx, run, ctx.Int("top"))

			return nil
		},
		cli.WithUsage(
			"bsctl n1 <nmdl_… | content-hash> [--top N] [--threshold-percent P]"),
		cli.WithFlag(cli.NewIntFlag("top", "", "how many to report", 10)),
		cli.WithFlag(cli.NewIntFlag(
			"threshold-percent", "",
			"loading above which a branch is reported; 0 uses the rating", 0)),
	)
}

// report prints a sweep worst-first.
func report(ctx cli.CommandContext, run sweep.Run, top int) {
	ranked := run.Ranked()

	concerning := run.Concerning()
	if len(concerning) == 0 {
		ctx.Println("  no contingency overloads anything at this threshold")

		return
	}

	ctx.Printf("  concerning %d\n\n", len(concerning))

	for index, candidate := range ranked {
		if index >= top {
			// Said rather than left implicit. A list that silently stops is a
			// list somebody reads as complete.
			ctx.Printf("  … %d more, use --top to see further\n", len(ranked)-top)

			break
		}

		if !candidate.Concerning() {
			break
		}

		// Unevaluated first and named as such. A radial outage that islands
		// part of the network is usually the most consequential thing on the
		// list and is exactly what a linear screen cannot answer — reporting
		// it as a loading of zero would bury it.
		if !candidate.Screenability.Evaluated() {
			ctx.Printf("  %-24s %s — the linear screen cannot evaluate this\n",
				candidate.ContingencyID, candidate.Screenability)

			continue
		}

		ctx.Printf("  %-24s worst %.1f%%  %d violation(s)\n",
			candidate.ContingencyID, worstOf(candidate)*100, len(candidate.Violations))

		for _, violation := range candidate.Violations {
			ctx.Printf("      branch %-4d %.1f%% of rating\n",
				violation.Branch, violation.Loading*100)
		}
	}
}

// worstOf reads a candidate's worst loading, which is absent when the screen
// could not evaluate it.
func worstOf(candidate sweep.Candidate) float64 {
	if candidate.WorstLoading == nil {
		return 0
	}

	return *candidate.WorstLoading
}

// worstBranch finds the most loaded branch in a solution.
func worstBranch(result solution.Solution) (string, float64) {
	var (
		name    string
		loading float64
	)

	for index, branch := range result.Branches {
		if branch.Loading > loading {
			loading = branch.Loading
			name = fmt.Sprintf("branch %d", index)
		}
	}

	return name, loading
}

// wireModel resolves the model named on the command line and converts it to
// the solver's SI contract.
//
// Read from the stored canonical bytes rather than re-parsed from a source
// file, so what is solved is what the content hash names — and so a result
// citing that hash cites the network it was actually computed from.
func wireModel(ctx cli.CommandContext, app forge.App) (*solverv1.NetworkModel, error) {
	engine, err := forge.Inject[*networkmodel.Engine](app.Container())
	if err != nil {
		return nil, fmt.Errorf("resolving the model engine: %w", err)
	}

	found, err := resolveModel(ctx, engine, ctx.Args()[0])
	if err != nil {
		return nil, err
	}

	encoded, err := engine.Encoding(ctx.Context(), found.ContentHash)
	if err != nil {
		return nil, err
	}

	decoded, err := canonical.Decode(encoded)
	if err != nil {
		return nil, err
	}

	return wire.Model(found.Name, decoded)
}

// startApp builds and starts an application, returning a cleanup.
func startApp(ctx context.Context) (forge.App, func(), error) {
	app, err := App()
	if err != nil {
		return nil, nil, err
	}

	if err := app.Start(ctx); err != nil {
		return nil, nil, fmt.Errorf("starting the application: %w", err)
	}

	return app, func() { _ = app.Stop(context.WithoutCancel(ctx)) }, nil
}
