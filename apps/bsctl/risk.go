package bsctl

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/cli"

	gid "github.com/xraph/blackstart/extensions/geo/id"
	"github.com/xraph/blackstart/extensions/risk"
	wid "github.com/xraph/blackstart/extensions/weather/id"
	"github.com/xraph/blackstart/pkg/id"
)

// riskCommand groups the risk subcommands.
func riskCommand() cli.Command {
	return cli.NewCommand(
		"risk",
		"Per-component failure probabilities from conductor loading",
		func(ctx cli.CommandContext) error {
			ctx.Println("usage: bsctl risk <compute|show>")

			return nil
		},
		cli.WithSubcommand(riskComputeCommand()),
		cli.WithSubcommand(riskShowCommand()),
	)
}

// riskComputeCommand runs the fragility models over one weather cycle.
func riskComputeCommand() cli.Command {
	return cli.NewCommand(
		"compute",
		"Run the fragility models over one weather cycle",
		func(ctx cli.CommandContext) error {
			engine, cleanup, err := riskEngine(ctx.Context())
			if err != nil {
				return err
			}

			defer cleanup()

			cycle, err := wid.ParseCycle(ctx.String("cycle"))
			if err != nil {
				return fmt.Errorf("bsctl: --cycle: %w", err)
			}

			result, err := engine.Compute(ctx.Context(), risk.ComputeRequest{CycleID: cycle})
			if err != nil {
				return err
			}

			ctx.Printf("computation %s\n", result.ComputationID)
			ctx.Printf("run         %s\n", orNone(result.RunID))
			ctx.Printf("spans       %d\n", result.Spans)
			ctx.Printf("structures  %d\n", result.Structures)
			// Unclassed and unhazarded are printed as separate lines, never
			// summed: one is a gap in the asset record (a structure whose Kind
			// maps to no fragility curve), the other a gap in weather coverage
			// (a span hazard never rated), and only one of them is anybody's
			// to fix by surveying more structures.
			ctx.Printf("unclassed   %d\n", result.Unclassed)
			ctx.Printf("unhazarded  %d\n", result.Unhazarded)
			ctx.Printf("values      %d\n", result.Values)
			ctx.Printf("scalar      %s\n", scalarLabel(result.Scalar))

			if result.Unclassed > 0 {
				ctx.Printf(
					"\n%d structures produced nothing because their Kind maps "+
						"to no fragility curve; a gap in the asset record.\n",
					result.Unclassed)
			}

			if result.Unhazarded > 0 {
				ctx.Printf(
					"%d spans produced nothing because hazard has no loading "+
						"for them over this cycle; a gap in the weather coverage.\n",
					result.Unhazarded)
			}

			return nil
		},
		cli.WithFlag(cli.NewStringFlag("cycle", "", "weather cycle identifier (wxcyc_…)", "")),
	)
}

// riskShowCommand prints a component's probability series.
func riskShowCommand() cli.Command {
	return cli.NewCommand(
		"show",
		"Show a component's failure probability series",
		func(ctx cli.CommandContext) error {
			if ctx.NArgs() < 1 {
				return errors.New("usage: bsctl risk show <span_… or strc_…> --issued RFC3339")
			}

			component, err := parseComponentArg(ctx.Args()[0])
			if err != nil {
				return err
			}

			engine, cleanup, err := riskEngine(ctx.Context())
			if err != nil {
				return err
			}

			defer cleanup()

			issued, err := time.Parse(time.RFC3339, ctx.String("issued"))
			if err != nil {
				return fmt.Errorf("bsctl: --issued must be an RFC 3339 timestamp: %w", err)
			}

			// A generous window rather than a required one, matching hazard's
			// own CLI reasoning: at the command line the useful question is
			// "what did this pass say", and the HTTP API keeps the window
			// required because it must never guess one.
			const horizon = 21 * 24 * time.Hour

			values, err := engine.ComponentSeries(
				ctx.Context(), component, nil, issued, issued, issued.Add(horizon))
			if err != nil {
				return err
			}

			if len(values) == 0 {
				ctx.Println("no values for that component and issuance")

				return nil
			}

			for _, got := range values {
				ctx.Printf("%s  %-18s %10.6f  scalar %s\n",
					got.ValidAt.Format(time.RFC3339), got.Quantity,
					got.Probability, scalarLabel(got.Scalar))
			}

			return nil
		},
		cli.WithUsage("bsctl risk show <span_… or strc_…> --issued RFC3339"),
		cli.WithFlag(cli.NewStringFlag("issued", "", "forecast issuance, RFC 3339", "")),
	)
}

// parseComponentArg reads a span or a structure identifier, whichever the
// argument names — the same either-or GET /api/v1/risk/components/:id/series
// accepts over HTTP.
func parseComponentArg(raw string) (id.ID, error) {
	if span, err := gid.ParseSpan(raw); err == nil {
		return span, nil
	}

	if structure, err := gid.ParseStructure(raw); err == nil {
		return structure, nil
	}

	return id.Nil, fmt.Errorf(
		"bsctl: %q is not a span or structure identifier (span_… or strc_…)", raw)
}

// scalarLabel renders the scalar with a calibration label, so any command
// that prints a probability never lets a reader assume it was calibrated. A
// risk figure that does not say whether it was calibrated invites being read
// as though it were.
func scalarLabel(scalar float64) string {
	if scalar == risk.ScalarUncalibrated {
		return fmt.Sprintf("%.4f (uncalibrated)", scalar)
	}

	return fmt.Sprintf("%.4f", scalar)
}

// riskEngine boots the application and resolves the engine.
func riskEngine(ctx context.Context) (*risk.Engine, func(), error) {
	app, err := App()
	if err != nil {
		return nil, nil, err
	}

	if err := app.Start(ctx); err != nil {
		return nil, nil, fmt.Errorf("starting the application: %w", err)
	}

	cleanup := func() { _ = app.Stop(context.WithoutCancel(ctx)) }

	engine, err := forge.Inject[*risk.Engine](app.Container())
	if err != nil {
		cleanup()

		return nil, nil, fmt.Errorf("resolving the risk engine: %w", err)
	}

	return engine, cleanup, nil
}
